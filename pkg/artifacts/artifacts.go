// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package artifacts

// GitHub Actions Artifacts V4 API Simple Description
//
// 1. Upload artifact
// 1.1. CreateArtifact
// Post: /twirp/github.actions.results.api.v1.ArtifactService/CreateArtifact
// Request:
// {
//     "workflow_run_backend_id": "21",
//     "workflow_job_run_backend_id": "49",
//     "name": "test",
//     "version": 4
// }
// Response:
// {
//     "ok": true,
//     "signedUploadUrl": "http://localhost:3000/twirp/github.actions.results.api.v1.ArtifactService/UploadArtifact?sig=mO7y35r4GyjN7fwg0DTv3-Fv1NDXD84KLEgLpoPOtDI=&expires=2024-01-23+21%3A48%3A37.20833956+%2B0100+CET&artifactName=test&taskID=75"
// }
// 1.2. Upload Zip Content to Blobstorage (unauthenticated request)
// PUT: http://localhost:3000/twirp/github.actions.results.api.v1.ArtifactService/UploadArtifact?sig=mO7y35r4GyjN7fwg0DTv3-Fv1NDXD84KLEgLpoPOtDI=&expires=2024-01-23+21%3A48%3A37.20833956+%2B0100+CET&artifactName=test&taskID=75&comp=block
// 1.3. Continue Upload Zip Content to Blobstorage (unauthenticated request), repeat until everything is uploaded
// PUT: http://localhost:3000/twirp/github.actions.results.api.v1.ArtifactService/UploadArtifact?sig=mO7y35r4GyjN7fwg0DTv3-Fv1NDXD84KLEgLpoPOtDI=&expires=2024-01-23+21%3A48%3A37.20833956+%2B0100+CET&artifactName=test&taskID=75&comp=appendBlock
// 1.4. Unknown xml payload to Blobstorage (unauthenticated request), ignored for now
// PUT: http://localhost:3000/twirp/github.actions.results.api.v1.ArtifactService/UploadArtifact?sig=mO7y35r4GyjN7fwg0DTv3-Fv1NDXD84KLEgLpoPOtDI=&expires=2024-01-23+21%3A48%3A37.20833956+%2B0100+CET&artifactName=test&taskID=75&comp=blockList
// 1.5. FinalizeArtifact
// Post: /twirp/github.actions.results.api.v1.ArtifactService/FinalizeArtifact
// Request
// {
//     "workflow_run_backend_id": "21",
//     "workflow_job_run_backend_id": "49",
//     "name": "test",
//     "size": "2097",
//     "hash": "sha256:b6325614d5649338b87215d9536b3c0477729b8638994c74cdefacb020a2cad4"
// }
// Response
// {
//     "ok": true,
//     "artifactId": "4"
// }
// 2. Download artifact
// 2.1. ListArtifacts and optionally filter by artifact exact name or id
// Post: /twirp/github.actions.results.api.v1.ArtifactService/ListArtifacts
// Request
// {
//     "workflow_run_backend_id": "21",
//     "workflow_job_run_backend_id": "49",
//     "name_filter": "test"
// }
// Response
// {
//     "artifacts": [
//         {
//             "workflowRunBackendId": "21",
//             "workflowJobRunBackendId": "49",
//             "databaseId": "4",
//             "name": "test",
//             "size": "2093",
//             "createdAt": "2024-01-23T00:13:28Z"
//         }
//     ]
// }
// 2.2. GetSignedArtifactURL get the URL to download the artifact zip file of a specific artifact
// Post: /twirp/github.actions.results.api.v1.ArtifactService/GetSignedArtifactURL
// Request
// {
//     "workflow_run_backend_id": "21",
//     "workflow_job_run_backend_id": "49",
//     "name": "test"
// }
// Response
// {
//     "signedUrl": "http://localhost:3000/twirp/github.actions.results.api.v1.ArtifactService/DownloadArtifact?sig=wHzFOwpF-6220-5CA0CIRmAX9VbiTC2Mji89UOqo1E8=&expires=2024-01-23+21%3A51%3A56.872846295+%2B0100+CET&artifactName=test&taskID=76"
// }
// 2.3. Download Zip from Blobstorage (unauthenticated request)
// GET: http://localhost:3000/twirp/github.actions.results.api.v1.ArtifactService/DownloadArtifact?sig=wHzFOwpF-6220-5CA0CIRmAX9VbiTC2Mji89UOqo1E8=&expires=2024-01-23+21%3A51%3A56.872846295+%2B0100+CET&artifactName=test&taskID=76

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/encoding/protojson"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	ArtifactV4RouteBase       = "/twirp/github.actions.results.api.v1.ArtifactService"
	ArtifactV4ContentEncoding = "application/zip"
)

type artifactV4Routes struct {
	prefix  string
	fs      WriteFS
	rfs     fs.FS
	AppURL  string
	baseDir string
}

type ArtifactContext struct {
	Req  *http.Request
	Resp http.ResponseWriter
}

func artifactNameToID(s string) int64 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return int64(h.Sum32())
}

func (c ArtifactContext) Error(status int, _ ...interface{}) {
	c.Resp.WriteHeader(status)
}

func (c ArtifactContext) JSON(status int, _ ...interface{}) {
	c.Resp.WriteHeader(status)
}

func validateRunIDV4(ctx *ArtifactContext, rawRunID string) (interface{}, int64, bool) {
	runID, err := strconv.ParseInt(rawRunID, 10, 64)
	if err != nil /* || task.Job.RunID != runID*/ {
		log.Error("Error runID not match")
		ctx.Error(http.StatusBadRequest, "run-id does not match")
		return nil, 0, false
	}
	return nil, runID, true
}

func RoutesV4(router *httprouter.Router, baseDir string, fsys WriteFS, rfs fs.FS) {
	route := &artifactV4Routes{
		fs:      fsys,
		rfs:     rfs,
		baseDir: baseDir,
		prefix:  ArtifactV4RouteBase,
	}
	router.POST(path.Join(ArtifactV4RouteBase, "CreateArtifact"), func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		route.AppURL = r.Host
		route.createArtifact(&ArtifactContext{
			Req:  r,
			Resp: w,
		})
	})
	router.POST(path.Join(ArtifactV4RouteBase, "FinalizeArtifact"), func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		route.finalizeArtifact(&ArtifactContext{
			Req:  r,
			Resp: w,
		})
	})
	router.POST(path.Join(ArtifactV4RouteBase, "ListArtifacts"), func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		route.listArtifacts(&ArtifactContext{
			Req:  r,
			Resp: w,
		})
	})
	router.POST(path.Join(ArtifactV4RouteBase, "GetSignedArtifactURL"), func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		route.AppURL = r.Host
		route.getSignedArtifactURL(&ArtifactContext{
			Req:  r,
			Resp: w,
		})
	})
	router.POST(path.Join(ArtifactV4RouteBase, "DeleteArtifact"), func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		route.AppURL = r.Host
		route.deleteArtifact(&ArtifactContext{
			Req:  r,
			Resp: w,
		})
	})
	router.PUT(path.Join(ArtifactV4RouteBase, "UploadArtifact"), func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		route.uploadArtifact(&ArtifactContext{
			Req:  r,
			Resp: w,
		})
	})
	router.GET(path.Join(ArtifactV4RouteBase, "DownloadArtifact"), func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		route.downloadArtifact(&ArtifactContext{
			Req:  r,
			Resp: w,
		})
	})
}

func (r artifactV4Routes) buildSignature(endp, expires, artifactName, filename string, taskID int64) []byte {
	mac := hmac.New(sha256.New, []byte{0xba, 0xdb, 0xee, 0xf0})
	mac.Write([]byte(endp))
	mac.Write([]byte(expires))
	mac.Write([]byte(artifactName))
	mac.Write([]byte(filename))
	mac.Write([]byte(fmt.Sprint(taskID)))
	return mac.Sum(nil)
}

func (r artifactV4Routes) buildArtifactURL(endp, artifactName, filename string, taskID int64) string {
	expires := time.Now().Add(60 * time.Minute).Format("2006-01-02 15:04:05.999999999 -0700 MST")
	uploadURL := "http://" + strings.TrimSuffix(r.AppURL, "/") + strings.TrimSuffix(r.prefix, "/") +
		"/" + endp + "?sig=" + base64.RawURLEncoding.EncodeToString(r.buildSignature(endp, expires, artifactName, filename, taskID)) + "&expires=" + url.QueryEscape(expires) + "&artifactName=" + url.QueryEscape(artifactName) + "&filename=" + url.QueryEscape(filename) + "&taskID=" + fmt.Sprint(taskID)
	return uploadURL
}

func (r artifactV4Routes) verifySignature(ctx *ArtifactContext, endp string) (int64, string, string, bool) {
	rawTaskID := ctx.Req.URL.Query().Get("taskID")
	sig := ctx.Req.URL.Query().Get("sig")
	expires := ctx.Req.URL.Query().Get("expires")
	artifactName := ctx.Req.URL.Query().Get("artifactName")
	filename := ctx.Req.URL.Query().Get("filename")
	dsig, _ := base64.RawURLEncoding.DecodeString(sig)
	taskID, _ := strconv.ParseInt(rawTaskID, 10, 64)

	expectedsig := r.buildSignature(endp, expires, artifactName, filename, taskID)
	if !hmac.Equal(dsig, expectedsig) {
		log.Errorf("Error unauthorized: endp=%q artifactName=%q filename=%q taskID=%d expires=%q sig=%q url=%s", endp, artifactName, filename, taskID, expires, sig, ctx.Req.URL.String())
		ctx.Error(http.StatusUnauthorized, "Error unauthorized")
		return -1, "", "", false
	}
	t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", expires)
	if err != nil || t.Before(time.Now()) {
		log.Error("Error link expired")
		ctx.Error(http.StatusUnauthorized, "Error link expired")
		return -1, "", "", false
	}
	return taskID, artifactName, filename, true
}

func (r *artifactV4Routes) parseProtbufBody(ctx *ArtifactContext, req protoreflect.ProtoMessage) bool {
	body, err := io.ReadAll(ctx.Req.Body)
	if err != nil {
		log.Errorf("Error decode request body: %v", err)
		ctx.Error(http.StatusInternalServerError, "Error decode request body")
		return false
	}
	err = protojson.Unmarshal(body, req)
	if err != nil {
		log.Errorf("Error decode request body: %v", err)
		ctx.Error(http.StatusInternalServerError, "Error decode request body")
		return false
	}
	return true
}

func (r *artifactV4Routes) sendProtbufBody(ctx *ArtifactContext, req protoreflect.ProtoMessage) {
	resp, err := protojson.Marshal(req)
	if err != nil {
		log.Errorf("Error encode response body: %v", err)
		ctx.Error(http.StatusInternalServerError, "Error encode response body")
		return
	}
	ctx.Resp.Header().Set("Content-Type", "application/json;charset=utf-8")
	ctx.Resp.WriteHeader(http.StatusOK)
	_, _ = ctx.Resp.Write(resp)
}

func (r *artifactV4Routes) createArtifact(ctx *ArtifactContext) {
	var req CreateArtifactRequest

	if ok := r.parseProtbufBody(ctx, &req); !ok {
		return
	}
	_, runID, ok := validateRunIDV4(ctx, req.WorkflowRunBackendId)
	if !ok {
		return
	}

	artifactName := req.Name

	// Determine MIME type: use provided value or default to application/zip
	mimeType := "application/zip"
	if req.MimeType != nil && req.MimeType.Value != "" {
		mimeType = req.MimeType.Value
	}

	// Determine content filename based on version and MIME type
	// v7 raw (mime != zip) → req.Name as-is
	// v7 zip (mime == zip) → req.Name + ".zip"
	// v4/legacy → req.Name + ".zip" (always archive)
	var contentFilename string
	if req.Version == 7 && !isZipMimeType(mimeType) {
		contentFilename = artifactName
	} else {
		contentFilename = artifactName + ".zip"
	}

	// Slugify directory and content filename
	slugDir := Slugify(artifactName)
	slugFile := Slugify(contentFilename)

	// Create directory structure: <runID>/<slugDir>/content/
	safeRunPath := safeResolve(r.baseDir, fmt.Sprint(runID))
	safePath := safeResolve(safeRunPath, slugDir)
	safeContentDir := safeResolve(safePath, "content")
	safeContentFile := safeResolve(safeContentDir, slugFile)

	// Create placeholder file for upload
	file, err := r.fs.OpenWritable(safeContentFile)
	if err != nil {
		log.Errorf("Failed to create artifact file: %v", err)
		ctx.Error(http.StatusInternalServerError, "Failed to create artifact file")
		return
	}
	file.Close()

	// Write metadata
	metadata := ArtifactMetadata{
		ArtifactName:      artifactName,
		OriginalFilename:  contentFilename,
		MimeType:          mimeType,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		WorkflowRunID:      req.WorkflowRunBackendId,
		WorkflowJobRunID:   req.WorkflowJobRunBackendId,
	}
	if err := WriteMetadata(safePath, metadata); err != nil {
		log.Errorf("Failed to write metadata: %v", err)
		ctx.Error(http.StatusInternalServerError, "Failed to write artifact metadata")
		return
	}

	// Build signed upload URL with filename parameter
	respData := CreateArtifactResponse{
		Ok:              true,
		SignedUploadUrl: r.buildArtifactURL("UploadArtifact", artifactName, slugFile, runID),
	}
	r.sendProtbufBody(ctx, &respData)
}

// isZipMimeType checks if a MIME type is a zip archive
func isZipMimeType(mimeType string) bool {
	zipMimes := []string{
		"application/zip",
		"application/x-zip",
		"application/x-zip-compressed",
	}
	for _, zm := range zipMimes {
		if mimeType == zm {
			return true
		}
	}
	return false
}

func (r *artifactV4Routes) uploadArtifact(ctx *ArtifactContext) {
	task, artifactName, filename, ok := r.verifySignature(ctx, "UploadArtifact")
	if !ok {
		return
	}

	comp := ctx.Req.URL.Query().Get("comp")
	switch comp {
	case "block", "appendBlock":

		safeRunPath := safeResolve(r.baseDir, fmt.Sprint(task))
		slugifiedArtifactName := Slugify(artifactName)
		safeArtifactPath := safeResolve(safeRunPath, slugifiedArtifactName)
		safeContentPath := safeResolve(safeArtifactPath, "content")
		safePath := safeResolve(safeContentPath, filename)

		file, err := r.fs.OpenAppendable(safePath)

		if err != nil {
			panic(err)
		}
		defer file.Close()

		writer, ok := file.(io.Writer)
		if !ok {
			panic(errors.New("File is not writable"))
		}

		if ctx.Req.Body == nil {
			panic(errors.New("No body given"))
		}

		_, err = io.Copy(writer, ctx.Req.Body)
		if err != nil {
			panic(err)
		}
		file.Close()
		ctx.JSON(http.StatusCreated, "appended")
	case "blocklist":
		ctx.JSON(http.StatusCreated, "created")
	}
}

func (r *artifactV4Routes) finalizeArtifact(ctx *ArtifactContext) {
	var req FinalizeArtifactRequest

	if ok := r.parseProtbufBody(ctx, &req); !ok {
		return
	}
	_, _, ok := validateRunIDV4(ctx, req.WorkflowRunBackendId)
	if !ok {
		return
	}

	respData := FinalizeArtifactResponse{
		Ok:         true,
		ArtifactId: artifactNameToID(req.Name),
	}
	r.sendProtbufBody(ctx, &respData)
}

func (r *artifactV4Routes) listArtifacts(ctx *ArtifactContext) {
	var req ListArtifactsRequest

	if ok := r.parseProtbufBody(ctx, &req); !ok {
		return
	}
	_, runID, ok := validateRunIDV4(ctx, req.WorkflowRunBackendId)
	if !ok {
		return
	}

	safePath := safeResolve(r.baseDir, fmt.Sprint(runID))

	entries, err := fs.ReadDir(r.rfs, safePath)
	if err != nil {
		panic(err)
	}

	list := []*ListArtifactsResponse_MonolithArtifact{}

	for _, entry := range entries {
		// Each entry is a directory with a slugified artifact name
		if !entry.IsDir() {
			continue
		}

		// Read metadata from the artifact directory
		safeArtifactPath := safeResolve(safePath, entry.Name())

		metadata, err := ReadMetadata(safeArtifactPath)
		if err != nil {
			// Skip artifacts with missing or invalid metadata
			log.Warnf("Skipping artifact with invalid metadata: %v", err)
			continue
		}

		// Check filters
		artifactName := metadata.ArtifactName
		id := artifactNameToID(artifactName)
		if (req.NameFilter == nil || req.NameFilter.Value == artifactName) && (req.IdFilter == nil || req.IdFilter.Value == id) {
			// Calculate size of content directory
			var size int64
			contentPath := safeResolve(safeArtifactPath, "content")
			if contentEntries, err := fs.ReadDir(r.rfs, contentPath); err == nil {
				for _, contentEntry := range contentEntries {
					if info, err := contentEntry.Info(); err == nil {
						size += info.Size()
					}
				}
			}

			data := &ListArtifactsResponse_MonolithArtifact{
				Name:                    artifactName,
				CreatedAt:               timestamppb.Now(),
				DatabaseId:              id,
				WorkflowRunBackendId:    req.WorkflowRunBackendId,
				WorkflowJobRunBackendId: req.WorkflowJobRunBackendId,
				Size:                    size,
			}
			if info, err := entry.Info(); err == nil {
				data.CreatedAt = timestamppb.New(info.ModTime())
			}
			list = append(list, data)
		}
	}

	respData := ListArtifactsResponse{
		Artifacts: list,
	}
	r.sendProtbufBody(ctx, &respData)
}

func (r *artifactV4Routes) getSignedArtifactURL(ctx *ArtifactContext) {
	var req GetSignedArtifactURLRequest

	if ok := r.parseProtbufBody(ctx, &req); !ok {
		return
	}
	_, runID, ok := validateRunIDV4(ctx, req.WorkflowRunBackendId)
	if !ok {
		return
	}

	artifactName := req.Name

	// Read metadata to determine the correct content filename
	safeRunPath := safeResolve(r.baseDir, fmt.Sprint(runID))
	safeArtifactPath := safeResolve(safeRunPath, Slugify(artifactName))

	metadata, err := ReadMetadata(safeArtifactPath)
	if err != nil {
		log.Errorf("Failed to read artifact metadata: %v", err)
		ctx.Error(http.StatusInternalServerError, "Failed to read artifact metadata")
		return
	}

	filename := Slugify(metadata.OriginalFilename)

	respData := GetSignedArtifactURLResponse{}

	respData.SignedUrl = r.buildArtifactURL("DownloadArtifact", artifactName, filename, runID)
	r.sendProtbufBody(ctx, &respData)
}

func (r *artifactV4Routes) downloadArtifact(ctx *ArtifactContext) {
	task, artifactName, filename, ok := r.verifySignature(ctx, "DownloadArtifact")
	if !ok {
		return
	}

	safeRunPath := safeResolve(r.baseDir, fmt.Sprint(task))
	slugifiedArtifactName := Slugify(artifactName)
	safeArtifactPath := safeResolve(safeRunPath, slugifiedArtifactName)

	// Read metadata to get MIME type and original name
	metadata, err := ReadMetadata(safeArtifactPath)
	if err != nil {
		log.Errorf("Failed to read artifact metadata: %v", err)
		ctx.Error(http.StatusInternalServerError, "Failed to read artifact metadata")
		return
	}

	safeContentPath := safeResolve(safeArtifactPath, "content")
	safePath := safeResolve(safeContentPath, filename)

	file, err := r.rfs.Open(safePath)
	if err != nil {
		log.Errorf("Failed to open artifact file: %v", err)
		ctx.Error(http.StatusInternalServerError, "Failed to download artifact")
		return
	}
	defer file.Close()

	// Set Content-Type based on MIME type
	if metadata.MimeType != "" {
		ctx.Resp.Header().Set("Content-Type", metadata.MimeType)
	}

	// Set Content-Disposition with original artifact name
	disposition := fmt.Sprintf("attachment; filename=%q", artifactName)
	ctx.Resp.Header().Set("Content-Disposition", disposition)

	_, _ = io.Copy(ctx.Resp, file)
}

func (r *artifactV4Routes) deleteArtifact(ctx *ArtifactContext) {
	var req DeleteArtifactRequest

	if ok := r.parseProtbufBody(ctx, &req); !ok {
		return
	}
	_, runID, ok := validateRunIDV4(ctx, req.WorkflowRunBackendId)
	if !ok {
		return
	}
	safeRunPath := safeResolve(r.baseDir, fmt.Sprint(runID))
	safePath := safeResolve(safeRunPath, req.Name)

	_ = os.RemoveAll(safePath)

	respData := DeleteArtifactResponse{
		Ok:         true,
		ArtifactId: artifactNameToID(req.Name),
	}
	r.sendProtbufBody(ctx, &respData)
}
