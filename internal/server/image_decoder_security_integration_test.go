package server_test

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/store"
)

func TestHTTPMalformedImageDecoderFixturesAreRejectedBeforeStorage(t *testing.T) {
	t.Parallel()
	e, root := newStorageHTTPEnv(t, 1<<20)

	tests := []struct {
		name     string
		filename string
		data     []byte
		project  bool
	}{
		{name: "profile BMP palette index", filename: "malformed.bmp", data: malformedBMPPaletteIndexFixture()},
		{name: "profile WebP dimension mismatch", filename: "malformed.webp", data: malformedWebPDimensionMismatchFixture(t)},
		{name: "project BMP palette index", filename: "malformed.bmp", data: malformedBMPPaletteIndexFixture(), project: true},
		{name: "project WebP dimension mismatch", filename: "malformed.webp", data: malformedWebPDimensionMismatchFixture(t), project: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			var body []byte
			if tc.project {
				code, body = e.doMultipartProjectImage(t, e.authToken, tc.filename, tc.data)
			} else {
				code, body = e.doMultipartProfileImage(t, e.authToken, tc.filename, tc.data)
			}
			if code != http.StatusBadRequest || !strings.Contains(string(body), "image could not be decoded") {
				t.Fatalf("upload code = %d body = %s", code, body)
			}
			assertNoStoredImages(t, e, root)
		})
	}
}

func assertNoStoredImages(t *testing.T, e *httpEnv, root string) {
	t.Helper()
	user, err := e.store.GetUser(e.ctx, e.adminID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.ProfileImageObjectID != nil || user.ProfileImageThumbnailObjectID != nil {
		t.Fatalf("profile image pointers = %v, %v; want nil", user.ProfileImageObjectID, user.ProfileImageThumbnailObjectID)
	}
	project, err := e.store.GetProject(e.ctx, e.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if project.ImageObjectID != nil || project.ImageThumbnailObjectID != nil {
		t.Fatalf("project image pointers = %v, %v; want nil", project.ImageObjectID, project.ImageThumbnailObjectID)
	}
	var objectCount int
	if err := e.pool.QueryRow(e.ctx, `
		SELECT count(*)
		FROM storage_objects
		WHERE owner_user_id = $1 OR project_id = $2
	`, e.adminID, e.projectID).Scan(&objectCount); err != nil {
		t.Fatalf("count storage objects: %v", err)
	}
	if objectCount != 0 {
		t.Fatalf("storage object metadata count = %d, want 0", objectCount)
	}
	for _, thumbnail := range []bool{false, true} {
		if _, err := e.store.GetUserProfileImageObject(e.ctx, e.adminID, thumbnail); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetUserProfileImageObject(thumbnail=%v) err = %v, want ErrNotFound", thumbnail, err)
		}
		if _, err := e.store.GetProjectImageObject(e.ctx, e.projectID, thumbnail); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetProjectImageObject(thumbnail=%v) err = %v, want ErrNotFound", thumbnail, err)
		}
	}
	if files := regularFilesUnder(t, root); len(files) != 0 {
		t.Fatalf("regular files after rejected image = %v, want none", files)
	}
}

// malformedBMPPaletteIndexFixture is a 1x1 8-bit BMP whose only pixel uses
// palette index 2 even though the color table has only indexes 0 and 1.
func malformedBMPPaletteIndexFixture() []byte {
	data := make([]byte, 66)
	copy(data, "BM")
	binary.LittleEndian.PutUint32(data[2:6], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[10:14], 62)
	binary.LittleEndian.PutUint32(data[14:18], 40)
	binary.LittleEndian.PutUint32(data[18:22], 1)
	binary.LittleEndian.PutUint32(data[22:26], 1)
	binary.LittleEndian.PutUint16(data[26:28], 1)
	binary.LittleEndian.PutUint16(data[28:30], 8)
	binary.LittleEndian.PutUint32(data[46:50], 2)
	copy(data[58:62], []byte{0xff, 0xff, 0xff, 0})
	data[62] = 2
	return data
}

// malformedWebPDimensionMismatchFixture prefixes a valid VP8L image with a VP8X
// header that claims a 1x1 canvas. The inner image has different dimensions.
func malformedWebPDimensionMismatchFixture(t *testing.T) []byte {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join("testdata", "webp_dimension_mismatch_base.base64"))
	if err != nil {
		t.Fatalf("read WebP fixture: %v", err)
	}
	valid, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatalf("decode WebP fixture: %v", err)
	}
	if len(valid) < 12 || string(valid[:4]) != "RIFF" || string(valid[8:12]) != "WEBP" {
		t.Fatalf("invalid base WebP fixture")
	}

	vp8x := []byte{
		'V', 'P', '8', 'X',
		10, 0, 0, 0,
		0, 0, 0, 0,
		0, 0, 0,
		0, 0, 0,
	}
	inner := valid[12:]
	fileSize := uint32(12 + len(vp8x) + len(inner) - 8)
	data := make([]byte, 0, int(fileSize)+8)
	data = append(data, "RIFF"...)
	data = binary.LittleEndian.AppendUint32(data, fileSize)
	data = append(data, "WEBP"...)
	data = append(data, vp8x...)
	data = append(data, inner...)
	return data
}
