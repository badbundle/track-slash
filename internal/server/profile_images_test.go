package server

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateProfileImageThumbnail(t *testing.T) {
	t.Parallel()
	thumbnail, err := generateProfileImageThumbnail(testProfilePNG(t, 4, 2))
	if err != nil {
		t.Fatalf("generateProfileImageThumbnail: %v", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(thumbnail))
	if err != nil {
		t.Fatalf("DecodeConfig thumbnail: %v", err)
	}
	if format != "png" || cfg.Width != profileThumbnailSize || cfg.Height != profileThumbnailSize {
		t.Fatalf("thumbnail format=%s size=%dx%d, want png %dx%d", format, cfg.Width, cfg.Height, profileThumbnailSize, profileThumbnailSize)
	}
}

func TestGenerateProfileImageThumbnailRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "corrupt", data: []byte("not an image"), want: "could not be decoded"},
		{name: "huge dimensions", data: testProfilePNGConfigOnly(9000, 10), want: "dimensions too large"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := generateProfileImageThumbnail(tc.data)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("generateProfileImageThumbnail err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseProfileImageUpload(t *testing.T) {
	t.Parallel()
	content := testProfilePNG(t, 2, 2)
	req := testProfileImageRequest(t, "face.png", content)
	rec := httptest.NewRecorder()
	upload, ok := parseProfileImageUpload(rec, req, int64(len(content)+1024))
	if !ok {
		t.Fatalf("parseProfileImageUpload failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if upload.Filename != "face.png" || upload.ContentType != "image/png" || !bytes.Equal(upload.Original, content) {
		t.Fatalf("upload = %+v", upload)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(upload.ThumbnailPNG))
	if err != nil {
		t.Fatalf("DecodeConfig thumbnail: %v", err)
	}
	if format != "png" || cfg.Width != profileThumbnailSize || cfg.Height != profileThumbnailSize {
		t.Fatalf("thumbnail format=%s size=%dx%d, want png %dx%d", format, cfg.Width, cfg.Height, profileThumbnailSize, profileThumbnailSize)
	}
}

func TestParseProfileImageUploadValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		req     *http.Request
		max     int64
		status  int
		message string
	}{
		{
			name:    "multipart required",
			req:     httptest.NewRequest(http.MethodPost, "/api/v1/me/profile-image", strings.NewReader("plain")),
			max:     1024,
			status:  http.StatusBadRequest,
			message: "multipart file required",
		},
		{
			name:    "unsupported type",
			req:     testProfileImageRequest(t, "vector.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)),
			max:     1024,
			status:  http.StatusBadRequest,
			message: "unsupported profile image type",
		},
		{
			name:    "corrupt allowed type",
			req:     testProfileImageRequest(t, "corrupt.png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'b', 'a', 'd'}),
			max:     1024,
			status:  http.StatusBadRequest,
			message: "could not be decoded",
		},
		{
			name:    "file too large",
			req:     testProfileImageRequest(t, "face.png", testProfilePNG(t, 2, 2)),
			max:     8,
			status:  http.StatusRequestEntityTooLarge,
			message: "file too large",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			if _, ok := parseProfileImageUpload(rec, tc.req, tc.max); ok {
				t.Fatalf("parseProfileImageUpload ok = true, want false")
			}
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.message) {
				t.Fatalf("status=%d body=%s, want %d containing %q", rec.Code, rec.Body.String(), tc.status, tc.message)
			}
		})
	}
}

func TestProfileImageContentTypeAllowed(t *testing.T) {
	t.Parallel()
	for _, contentType := range []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp", " IMAGE/PNG "} {
		if !profileImageContentTypeAllowed(contentType) {
			t.Fatalf("profileImageContentTypeAllowed(%q) = false, want true", contentType)
		}
	}
	for _, contentType := range []string{"image/svg+xml", "image/avif", "text/plain", "application/octet-stream", ""} {
		if profileImageContentTypeAllowed(contentType) {
			t.Fatalf("profileImageContentTypeAllowed(%q) = true, want false", contentType)
		}
	}
}

func testProfilePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(20 + x*30), G: uint8(40 + y*30), B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func testProfileImageRequest(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/profile-image", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func testProfilePNGConfigOnly(width, height uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8
	ihdr[9] = 2
	testProfilePNGChunk(&buf, "IHDR", ihdr)
	return buf.Bytes()
}

func testProfilePNGChunk(buf *bytes.Buffer, kind string, data []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(data)))
	buf.Write(length[:])
	buf.WriteString(kind)
	buf.Write(data)
	crc := crc32.NewIEEE()
	_, _ = crc.Write([]byte(kind))
	_, _ = crc.Write(data)
	var sum [4]byte
	binary.BigEndian.PutUint32(sum[:], crc.Sum32())
	buf.Write(sum[:])
}
