package handlers

import (
	"crypto/rand"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"strings"

	"github.com/mkende/screenshotter/server/internal/auth"
	"github.com/mkende/screenshotter/server/internal/db"
	"github.com/mkende/screenshotter/server/internal/storage"
)

const idChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// Upload handles POST /upload: saves the PNG, records it in the DB, and
// returns a redirect_url for the extension to navigate to.
func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	identity := auth.FromContext(r.Context())
	if identity == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	maxBytes := h.cfg.Server.MaxUploadMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "upload too large or malformed")
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck

	file, _, err := r.FormFile("image")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "missing 'image' field")
		return
	}
	defer file.Close()

	var sourceURL *string
	if s := strings.TrimSpace(r.FormValue("source_url")); s != "" {
		sourceURL = &s
	}

	id, err := generateID(h.cfg.ID.Length)
	if err != nil {
		slog.Error("generate image id", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	filePath, err := h.storage.Save(id, file)
	if err != nil {
		if errors.Is(err, storage.ErrNotPNG) {
			writeJSONError(w, http.StatusBadRequest, "uploaded file is not a valid PNG image")
			return
		}
		slog.Error("save image", "id", id, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to save image")
		return
	}

	if err := h.upsertUser(r.Context(), identity); err != nil {
		h.storage.Delete(id) //nolint:errcheck
		slog.Error("upsert user on upload", "email", identity.Email, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	img := db.Image{
		ID:        id,
		OwnerID:   identity.Email,
		SourceURL: sourceURL,
		FilePath:  filePath,
	}
	if err := h.db.InsertImage(r.Context(), img); err != nil {
		h.storage.Delete(id) //nolint:errcheck
		slog.Error("insert image record", "id", id, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to record image")
		return
	}

	redirectURL := h.cfg.CanonicalAddress + "/" + id
	writeJSON(w, http.StatusOK, map[string]string{"redirect_url": redirectURL})
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
