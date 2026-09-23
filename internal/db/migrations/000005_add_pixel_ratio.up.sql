-- How many pixels of the stored image cover one CSS pixel of the page it was
-- captured from: 2 for a capture taken on a Retina display at 100% zoom. The
-- view page divides by it so an image is shown at the size it appeared on
-- screen. 1, the default, is the size-is-resolution case every row had before.
ALTER TABLE images ADD COLUMN pixel_ratio REAL NOT NULL DEFAULT 1;
