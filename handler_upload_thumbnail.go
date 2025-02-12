package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "thumbnail not found in request", err)
		return
	}
	defer file.Close()

	contentType := strings.Trim(header.Header.Get("Content-Type"), "\r\n\t ")
	if contentType == "" {
		respondWithError(w, http.StatusBadRequest, "missing Content-Type header for thumbnail", err)
		return
	}

	if contentType != "image/jpeg" && contentType != "image/png" {
		respondWithError(w, http.StatusUnsupportedMediaType, "bad Content-Type", err)
		return
	}

	fileData, err := io.ReadAll(file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to read thumbnail data", err)
		return
	}

	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "unable to find video", err)
		return
	}

	if videoMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "current user is not authorized", err)
		return
	}

	fileExt := strings.Split(contentType, "/")[1]

	randBytesSize := 2 << 4
	randBytes := make([]byte, randBytesSize)

	bytesRead, err := rand.Read(randBytes)
	if bytesRead != randBytesSize {
		respondWithError(w, http.StatusInternalServerError, "generic internal server error", err)
		return
	}

	fileName := fmt.Sprintf("%s.%s", base64.RawURLEncoding.EncodeToString(randBytes), fileExt)
	filePath := filepath.Join(cfg.assetsRoot, fileName)

	fileHandle, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to create file to save thumbnail", err)
		return
	}

	_, err = fileHandle.Write(fileData)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "failed or partial write of thumbnail contents", err)
		return
	}

	thumbnailURL := fmt.Sprintf("http://localhost:%s/%s", cfg.port, filePath)
	videoMetadata.ThumbnailURL = &thumbnailURL

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMetadata)
}
