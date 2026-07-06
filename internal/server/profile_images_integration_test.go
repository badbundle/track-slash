package server_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/store"
)

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(40 + x*20), G: uint8(90 + y*20), B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func (e *httpEnv) doMultipartProfileImage(t *testing.T, token, filename string, content []byte) (int, []byte) {
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
	req, err := http.NewRequestWithContext(e.ctx, http.MethodPost, e.ts.URL+apiPath("/me/profile-image"), &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do multipart profile image: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res.StatusCode, body
}

func TestHTTPProfileImageUploadContentVisibilityAndDelete(t *testing.T) {
	t.Parallel()
	e, root := newStorageHTTPEnv(t, 1<<20)
	content := testPNG(t, 3, 2)

	code, body := e.doMultipartProfileImage(t, e.authToken, "face.png", content)
	if code != http.StatusOK {
		t.Fatalf("upload code = %d body = %s", code, body)
	}
	user := decode[model.User](t, body)
	if user.ID != e.adminID || user.ProfileImageObjectID == nil || user.ProfileImageThumbnailObjectID == nil {
		t.Fatalf("uploaded user = %+v", user)
	}
	if *user.ProfileImageObjectID == *user.ProfileImageThumbnailObjectID {
		t.Fatalf("profile image ids should be distinct: %+v", user)
	}

	original, err := e.store.GetUserProfileImageObject(e.ctx, e.adminID, false)
	if err != nil {
		t.Fatalf("GetUserProfileImageObject original: %v", err)
	}
	thumbnail, err := e.store.GetUserProfileImageObject(e.ctx, e.adminID, true)
	if err != nil {
		t.Fatalf("GetUserProfileImageObject thumbnail: %v", err)
	}
	for _, object := range []model.StorageObject{original, thumbnail} {
		if object.ProjectID != uuid.Nil || object.OwnerUserID == nil || *object.OwnerUserID != e.adminID || object.Number != 0 || object.Ref != "" {
			t.Fatalf("profile object = %+v, want user-owned unnumbered object", object)
		}
		if !strings.HasPrefix(object.ObjectKey, "users/"+e.adminID.String()+"/profile-images/"+object.ID.String()+"/") {
			t.Fatalf("object key = %q, want user profile-image key", object.ObjectKey)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(object.ObjectKey))); err != nil {
			t.Fatalf("stored profile object %s: %v", object.ObjectKey, err)
		}
	}

	res, gotOriginal := e.doRaw(t, e.authToken, http.MethodGet, "/users/"+e.adminID.String()+"/profile-image/content", nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("original content code = %d body = %s", res.StatusCode, gotOriginal)
	}
	if !bytes.Equal(gotOriginal, content) {
		t.Fatalf("original content = %d bytes, want upload bytes", len(gotOriginal))
	}
	if got := res.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("original Content-Type = %q, want image/png", got)
	}

	viewer, viewerToken := e.mustUserToken(t, "profile-viewer")
	res, gotThumb := e.doRaw(t, viewerToken, http.MethodGet, "/users/"+e.adminID.String()+"/profile-image/thumbnail/content", nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("viewer thumbnail code = %d body = %s viewer=%+v", res.StatusCode, gotThumb, viewer)
	}
	if got := res.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("thumbnail Content-Type = %q, want image/png", got)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(gotThumb))
	if err != nil {
		t.Fatalf("DecodeConfig thumbnail: %v", err)
	}
	if format != "png" || cfg.Width != 128 || cfg.Height != 128 {
		t.Fatalf("thumbnail format=%s size=%dx%d, want png 128x128", format, cfg.Width, cfg.Height)
	}

	res, unauthBody := e.doRaw(t, "", http.MethodGet, "/users/"+e.adminID.String()+"/profile-image/content", nil, "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauth content code = %d body = %s", res.StatusCode, unauthBody)
	}
	code, body = e.do(t, http.MethodGet, e.projectObjectsPath()+"/object-0", nil)
	if code != http.StatusBadRequest && code != http.StatusNotFound {
		t.Fatalf("project object route exposed user object code = %d body = %s", code, body)
	}

	code, body = e.do(t, http.MethodDelete, "/me/profile-image", nil)
	if code != http.StatusOK {
		t.Fatalf("delete code = %d body = %s", code, body)
	}
	deletedUser := decode[model.User](t, body)
	if deletedUser.ProfileImageObjectID != nil || deletedUser.ProfileImageThumbnailObjectID != nil {
		t.Fatalf("delete user profile ids = %+v, want nil", deletedUser)
	}
	for _, object := range []model.StorageObject{original, thumbnail} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(object.ObjectKey))); !os.IsNotExist(err) {
			t.Fatalf("deleted backend object %s stat err = %v, want not exist", object.ObjectKey, err)
		}
	}
	res, body = e.doRaw(t, e.authToken, http.MethodGet, "/users/"+e.adminID.String()+"/profile-image/content", nil, "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted content code = %d body = %s", res.StatusCode, body)
	}
}

func TestHTTPProfileImageUploadValidationAndDisabledStorage(t *testing.T) {
	t.Parallel()
	e, _ := newStorageHTTPEnv(t, 16)
	content := testPNG(t, 2, 2)

	code, body := e.doMultipartProfileImage(t, "", "face.png", content)
	if code != http.StatusUnauthorized {
		t.Fatalf("unauth upload code = %d body = %s", code, body)
	}
	code, body = e.doMultipartProfileImage(t, e.authToken, "face.png", content)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload code = %d body = %s", code, body)
	}

	largeEnough, _ := newStorageHTTPEnv(t, 1<<20)
	code, body = largeEnough.doMultipartProfileImage(t, largeEnough.authToken, "notes.txt", []byte("not an image"))
	if code != http.StatusBadRequest || !strings.Contains(string(body), "unsupported profile image type") {
		t.Fatalf("text upload code = %d body = %s", code, body)
	}
	code, body = largeEnough.doMultipartProfileImage(t, largeEnough.authToken, "corrupt.png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'b', 'a', 'd'})
	if code != http.StatusBadRequest || !strings.Contains(string(body), "could not be decoded") {
		t.Fatalf("corrupt image code = %d body = %s", code, body)
	}
	code, body = largeEnough.doMultipartProfileImage(t, largeEnough.authToken, "vector.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
	if code != http.StatusBadRequest || !strings.Contains(string(body), "unsupported profile image type") {
		t.Fatalf("svg upload code = %d body = %s", code, body)
	}

	disabled := newHTTPEnv(t)
	code, body = disabled.doMultipartProfileImage(t, disabled.authToken, "face.png", content)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("disabled storage upload code = %d body = %s", code, body)
	}
	originalID := uuid.New()
	thumbnailID := uuid.New()
	original, err := disabled.store.CreateUserStorageObject(disabled.ctx, store.CreateUserStorageObjectParams{
		ID:          originalID,
		OwnerUserID: disabled.adminID,
		Backend:     "local",
		Bucket:      "local",
		ObjectKey:   "users/" + disabled.adminID.String() + "/profile-images/" + originalID.String() + "/original",
		Filename:    "face.png",
		ContentType: "image/png",
		ByteSize:    1,
		SHA256:      strings.Repeat("a", 64),
		CreatedByID: disabled.adminID,
	})
	if err != nil {
		t.Fatalf("CreateUserStorageObject disabled original: %v", err)
	}
	thumbnail, err := disabled.store.CreateUserStorageObject(disabled.ctx, store.CreateUserStorageObjectParams{
		ID:          thumbnailID,
		OwnerUserID: disabled.adminID,
		Backend:     "local",
		Bucket:      "local",
		ObjectKey:   "users/" + disabled.adminID.String() + "/profile-images/" + thumbnailID.String() + "/thumbnail",
		Filename:    "profile-thumbnail.png",
		ContentType: "image/png",
		ByteSize:    1,
		SHA256:      strings.Repeat("b", 64),
		CreatedByID: disabled.adminID,
	})
	if err != nil {
		t.Fatalf("CreateUserStorageObject disabled thumbnail: %v", err)
	}
	if _, err := disabled.store.ReplaceUserProfileImage(disabled.ctx, disabled.adminID, original.ID, thumbnail.ID); err != nil {
		t.Fatalf("ReplaceUserProfileImage disabled: %v", err)
	}
	res, body := disabled.doRaw(t, disabled.authToken, http.MethodGet, "/users/"+disabled.adminID.String()+"/profile-image/content", nil, "")
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("disabled storage content code = %d body = %s", res.StatusCode, body)
	}
}
