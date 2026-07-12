package server_test

import (
	"bytes"
	"image"
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

func (e *httpEnv) doMultipartProjectImage(t *testing.T, token, filename string, content []byte) (int, []byte) {
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
	req, err := http.NewRequestWithContext(e.ctx, http.MethodPost, e.ts.URL+apiPath(e.projectPath()+"/image"), &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do multipart project image: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return res.StatusCode, body
}

func TestHTTPProjectImageUploadContentIsolationAndDelete(t *testing.T) {
	t.Parallel()
	e, root := newStorageHTTPEnv(t, 1<<20)
	content := testPNG(t, 3, 2)

	code, body := e.doMultipartProjectImage(t, e.authToken, "project.png", content)
	if code != http.StatusOK {
		t.Fatalf("upload code = %d body = %s", code, body)
	}
	project := decode[model.Project](t, body)
	if project.ID != e.projectID || project.ImageObjectID == nil || project.ImageThumbnailObjectID == nil || *project.ImageObjectID == *project.ImageThumbnailObjectID {
		t.Fatalf("uploaded project = %+v", project)
	}

	original, err := e.store.GetProjectImageObject(e.ctx, e.projectID, false)
	if err != nil {
		t.Fatalf("GetProjectImageObject original: %v", err)
	}
	thumbnail, err := e.store.GetProjectImageObject(e.ctx, e.projectID, true)
	if err != nil {
		t.Fatalf("GetProjectImageObject thumbnail: %v", err)
	}
	for _, object := range []model.StorageObject{original, thumbnail} {
		if object.ProjectID != e.projectID || object.OwnerUserID != nil || object.Number <= 0 || object.Ref == "" {
			t.Fatalf("project image object = %+v, want numbered project-owned object", object)
		}
		if !strings.HasPrefix(object.ObjectKey, "projects/"+e.projectID.String()+"/images/"+object.ID.String()+"/") {
			t.Fatalf("object key = %q, want project image key", object.ObjectKey)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(object.ObjectKey))); err != nil {
			t.Fatalf("stored project image object %s: %v", object.ObjectKey, err)
		}
		code, routeBody := e.do(t, http.MethodGet, e.projectObjectsPath()+"/"+object.Ref, nil)
		if code != http.StatusNotFound {
			t.Fatalf("generic object route exposed image %s: code=%d body=%s", object.Ref, code, routeBody)
		}
	}
	objects, more, err := e.store.ListStorageObjects(e.ctx, store.ListStorageObjectsParams{ProjectID: e.projectID, Limit: 10})
	if err != nil || more || len(objects) != 0 {
		t.Fatalf("ListStorageObjects = %+v more=%v err=%v, want hidden image pair", objects, more, err)
	}

	res, gotOriginal := e.doRaw(t, e.authToken, http.MethodGet, e.projectPath()+"/image/content", nil, "")
	if res.StatusCode != http.StatusOK || !bytes.Equal(gotOriginal, content) {
		t.Fatalf("original content code=%d bytes=%d body=%s", res.StatusCode, len(gotOriginal), gotOriginal)
	}
	_, memberToken := e.mustProjectMemberToken(t, "project-image-viewer")
	res, gotThumbnail := e.doRaw(t, memberToken, http.MethodGet, e.projectPath()+"/image/thumbnail/content", nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("member thumbnail code = %d body = %s", res.StatusCode, gotThumbnail)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(gotThumbnail))
	if err != nil || format != "png" || cfg.Width != 128 || cfg.Height != 128 {
		t.Fatalf("thumbnail format=%s size=%dx%d err=%v, want png 128x128", format, cfg.Width, cfg.Height, err)
	}
	_, outsiderToken := e.mustUserToken(t, "project-image-outsider")
	res, outsiderBody := e.doRaw(t, outsiderToken, http.MethodGet, e.projectPath()+"/image/content", nil, "")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("outsider content code = %d body = %s", res.StatusCode, outsiderBody)
	}
	previousObjects := []model.StorageObject{original, thumbnail}
	code, body = e.doMultipartProjectImage(t, e.authToken, "replacement.png", testPNG(t, 2, 3))
	if code != http.StatusOK {
		t.Fatalf("replacement upload code = %d body = %s", code, body)
	}
	replacement := decode[model.Project](t, body)
	if replacement.ImageObjectID == nil || replacement.ImageThumbnailObjectID == nil || *replacement.ImageObjectID == original.ID || *replacement.ImageThumbnailObjectID == thumbnail.ID {
		t.Fatalf("replacement project = %+v, want new image pair", replacement)
	}
	for _, object := range previousObjects {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(object.ObjectKey))); !os.IsNotExist(err) {
			t.Fatalf("replaced backend object %s stat err = %v, want not exist", object.ObjectKey, err)
		}
	}
	original, err = e.store.GetProjectImageObject(e.ctx, e.projectID, false)
	if err != nil {
		t.Fatalf("GetProjectImageObject replacement original: %v", err)
	}
	thumbnail, err = e.store.GetProjectImageObject(e.ctx, e.projectID, true)
	if err != nil {
		t.Fatalf("GetProjectImageObject replacement thumbnail: %v", err)
	}

	code, body = e.do(t, http.MethodDelete, e.projectPath()+"/image", nil)
	if code != http.StatusOK {
		t.Fatalf("delete code = %d body = %s", code, body)
	}
	deletedProject := decode[model.Project](t, body)
	if deletedProject.ImageObjectID != nil || deletedProject.ImageThumbnailObjectID != nil {
		t.Fatalf("delete project image ids = %+v, want nil", deletedProject)
	}
	for _, object := range []model.StorageObject{original, thumbnail} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(object.ObjectKey))); !os.IsNotExist(err) {
			t.Fatalf("deleted backend object %s stat err = %v, want not exist", object.ObjectKey, err)
		}
	}
	res, body = e.doRaw(t, e.authToken, http.MethodGet, e.projectPath()+"/image/content", nil, "")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted content code = %d body = %s", res.StatusCode, body)
	}
}

func TestHTTPProjectImageUploadValidationAccessAndDisabledStorage(t *testing.T) {
	t.Parallel()
	e, _ := newStorageHTTPEnv(t, 16)
	content := testPNG(t, 2, 2)

	code, body := e.doMultipartProjectImage(t, "", "project.png", content)
	if code != http.StatusUnauthorized {
		t.Fatalf("unauth upload code = %d body = %s", code, body)
	}
	_, outsiderToken := e.mustUserToken(t, "project-image-upload-outsider")
	code, body = e.doMultipartProjectImage(t, outsiderToken, "project.png", content)
	if code != http.StatusForbidden {
		t.Fatalf("outsider upload code = %d body = %s", code, body)
	}
	code, body = e.doMultipartProjectImage(t, e.authToken, "project.png", content)
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload code = %d body = %s", code, body)
	}

	largeEnough, _ := newStorageHTTPEnv(t, 1<<20)
	code, body = largeEnough.doMultipartProjectImage(t, largeEnough.authToken, "notes.txt", []byte("not an image"))
	if code != http.StatusBadRequest || !strings.Contains(string(body), "unsupported project image type") {
		t.Fatalf("text upload code = %d body = %s", code, body)
	}
	code, body = largeEnough.doMultipartProjectImage(t, largeEnough.authToken, "corrupt.png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 'b', 'a', 'd'})
	if code != http.StatusBadRequest || !strings.Contains(string(body), "project image could not be decoded") {
		t.Fatalf("corrupt upload code = %d body = %s", code, body)
	}

	disabled := newHTTPEnv(t)
	code, body = disabled.doMultipartProjectImage(t, disabled.authToken, "project.png", content)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("disabled upload code = %d body = %s", code, body)
	}
	project, err := disabled.store.GetProject(disabled.ctx, disabled.projectID)
	if err != nil {
		t.Fatalf("GetProject disabled: %v", err)
	}
	originalID := uuid.New()
	original, err := disabled.store.CreateStorageObject(disabled.ctx, store.CreateStorageObjectParams{
		ID: originalID, ProjectID: project.ID, Backend: "local", Bucket: "local",
		ObjectKey: "projects/" + project.ID.String() + "/images/" + originalID.String() + "/original",
		Filename:  "project.png", ContentType: "image/png", ByteSize: 1, SHA256: strings.Repeat("a", 64), CreatedByID: disabled.adminID,
	})
	if err != nil {
		t.Fatalf("CreateStorageObject disabled original: %v", err)
	}
	thumbnailID := uuid.New()
	thumbnail, err := disabled.store.CreateStorageObject(disabled.ctx, store.CreateStorageObjectParams{
		ID: thumbnailID, ProjectID: project.ID, Backend: "local", Bucket: "local",
		ObjectKey: "projects/" + project.ID.String() + "/images/" + thumbnailID.String() + "/thumbnail",
		Filename:  "project-thumbnail.png", ContentType: "image/png", ByteSize: 1, SHA256: strings.Repeat("b", 64), CreatedByID: disabled.adminID,
	})
	if err != nil {
		t.Fatalf("CreateStorageObject disabled thumbnail: %v", err)
	}
	if _, err := disabled.store.ReplaceProjectImage(disabled.ctx, project.ID, original.ID, thumbnail.ID); err != nil {
		t.Fatalf("ReplaceProjectImage disabled: %v", err)
	}
	res, body := disabled.doRaw(t, disabled.authToken, http.MethodGet, disabled.projectPath()+"/image/content", nil, "")
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("disabled content code = %d body = %s", res.StatusCode, body)
	}
}

func TestUIProjectImageUploadAndDeleteRerenderAboutPanel(t *testing.T) {
	t.Parallel()
	e, _ := newStorageHTTPEnv(t, 1<<20)
	_, token := e.mustProjectMemberToken(t, "ui-project-image")

	res := e.uiDoMultipartContext(t, e.projectPath()+"/image", token, nil, "project.png", string(testPNG(t, 3, 2)))
	defer res.Body.Close()
	body := readBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("UI upload code = %d body = %s", res.StatusCode, body)
	}
	project, err := e.store.GetProject(e.ctx, e.projectID)
	if err != nil {
		t.Fatalf("GetProject after UI upload: %v", err)
	}
	if project.ImageThumbnailObjectID == nil {
		t.Fatalf("UI upload project missing thumbnail id: %+v", project)
	}
	for _, want := range []string{
		`aria-label="Breadcrumb"`,
		`>About</span>`,
		e.projectPath() + `/image/thumbnail/content?v=` + project.ImageThumbnailObjectID.String(),
		`action="` + e.projectPath() + `/image/delete"`,
		`rounded-md object-cover`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("UI upload panel missing %q: %s", want, body)
		}
	}

	res = e.uiDoNoRedirect(t, http.MethodPost, e.projectPath()+"/image/delete", token, nil)
	defer res.Body.Close()
	body = readBody(t, res)
	if res.StatusCode != http.StatusOK || strings.Contains(body, `action="`+e.projectPath()+`/image/delete"`) || strings.Contains(body, e.projectPath()+"/image/thumbnail/content") {
		t.Fatalf("UI delete code = %d body = %s", res.StatusCode, body)
	}
}
