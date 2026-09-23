package handlers

import (
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"math"
	"math/big"
	"net/http"
	"strconv"

	"github.com/mkende/screenshotter_server/internal/auth"
	"github.com/mkende/screenshotter_server/internal/db"
	"github.com/mkende/screenshotter_server/internal/httputil"
	"github.com/mkende/screenshotter_server/internal/storage"
)

const (
	idChars      = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	maxIDRetries = 3 // maximum attempts to find a collision-free ID

	// Bounds on the uploaded pixel_ratio, matching the extension's own. A
	// display packs a handful of pixels into a CSS pixel at most, so anything
	// outside this is a bad measurement or a malformed request, and is taken
	// as 1 — the image displays at its own size, as it did before the field
	// existed.
	minPixelRatio = 1.0 / 8
	maxPixelRatio = 8
)

// Upload handles POST /upload: saves the PNG, records it in the DB, and
// returns a redirect_url for the extension to navigate to.
func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	identity := auth.FromContext(r.Context())
	if identity == nil {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	maxBytes := h.cfg.Server.MaxUploadMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		httputil.WriteJSONError(w, http.StatusRequestEntityTooLarge, "upload too large or malformed")
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	file, _, err := r.FormFile("image")
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "missing 'image' field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		slog.Error("read upload", "err", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := storage.ValidatePNG(data); err != nil {
		if errors.Is(err, storage.ErrNotPNG) {
			httputil.WriteJSONError(w, http.StatusBadRequest, "uploaded file is not a valid PNG image")
			return
		}
		if errors.Is(err, storage.ErrImageTooLarge) {
			httputil.WriteJSONError(w, http.StatusBadRequest, "image dimensions are too large")
			return
		}
		slog.Error("validate image", "err", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	pixelRatio := parsePixelRatio(r.FormValue("pixel_ratio"))

	// source_url is auto-populated by the extension from the captured tab's URL.
	// A disallowed scheme is dropped rather than failing the upload, since the
	// user did not type it and the screenshot itself is still valid.
	sourceURL, err := h.parseSourceURL(r.FormValue("source_url"))
	if err != nil {
		slog.Debug("dropping source_url with disallowed scheme on upload", "err", err)
		sourceURL = nil
	}

	if err := h.upsertUser(r.Context(), identity); err != nil {
		slog.Error("upsert user on upload", "email", identity.Email, "err", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Insert the DB row first so the unique constraint acts as the dedup gate.
	// Retry a few times on ID collision (extremely rare in practice but possible
	// at short configured ID lengths).
	var id string
	inserted := false
	for range maxIDRetries {
		id, err = generateID(h.cfg.ID.Length)
		if err != nil {
			slog.Error("generate image id", "err", err)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		err = h.db.InsertImage(r.Context(), db.Image{
			ID:         id,
			OwnerID:    identity.Email,
			SourceURL:  sourceURL,
			FilePath:   id + ".png",
			PixelRatio: pixelRatio,
		})
		if err == nil {
			inserted = true
			break
		}
		if errors.Is(err, db.ErrDuplicateID) {
			slog.Warn("image ID collision, retrying", "id", id)
			continue
		}
		slog.Error("insert image record", "id", id, "err", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "failed to record image")
		return
	}
	if !inserted {
		slog.Error("insert image record: all retries exhausted", "attempts", maxIDRetries)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "failed to record image")
		return
	}

	// Write the file only after the DB row is committed. O_EXCL inside SaveNew
	// provides a safety net against corrupt state (orphaned file with no DB row).
	if err := h.storage.SaveNew(id, data); err != nil {
		h.db.DeleteImage(r.Context(), id, identity.Email) //nolint:errcheck
		if errors.Is(err, storage.ErrIDCollision) {
			slog.Error("save image: file exists despite successful DB insert", "id", id)
			httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		slog.Error("save image", "id", id, "err", err)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "failed to save image")
		return
	}

	redirectURL := h.cfg.CanonicalAddress + "/" + id
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"redirect_url": redirectURL})
}

// parsePixelRatio reads the uploaded pixel_ratio: how many pixels of the image
// cover one CSS pixel of the page it was captured from. Anything missing,
// unparseable or out of range is taken as 1 rather than failing the upload —
// an older extension does not send the field at all, and a screenshot is worth
// keeping even when its scale cannot be trusted. It is only ever used to
// divide the image's own dimensions for display.
func parsePixelRatio(raw string) float64 {
	if raw == "" {
		return 1
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || v < minPixelRatio || v > maxPixelRatio {
		slog.Debug("ignoring out-of-range pixel_ratio on upload", "value", raw)
		return 1
	}
	return v
}

// generateID returns a cryptographically random alphanumeric string of length n.
func generateID(n int) (string, error) {
	b := make([]byte, n)
	max := big.NewInt(int64(len(idChars)))
	for i := range b {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = idChars[idx.Int64()]
	}
	return string(b), nil
}
