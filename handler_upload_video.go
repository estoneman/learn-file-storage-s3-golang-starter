package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const uploadLimit = 1 << 30
	http.MaxBytesReader(w, r.Body, uploadLimit)

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

	log.Printf("uploading thumbnail for video %s by user %s", videoID, userID)

	videoMetadata, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "unable to find video", err)
		return
	}

	if videoMetadata.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "current user is not authorized", err)
		return
	}

	videoFormFile, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "video not found in request", err)
		return
	}
	defer videoFormFile.Close()

	contentType := strings.Trim(header.Header.Get("Content-Type"), "\r\n\t ")
	if contentType == "" {
		respondWithError(w, http.StatusBadRequest, "missing Content-Type header for thumbnail", err)
		return
	}

	if contentType != "video/mp4" {
		respondWithError(w, http.StatusUnsupportedMediaType, "bad Content-Type", err)
		return
	}

	const tmpFileName = "tubely-upload.mp4"
	tmpFile, err := os.CreateTemp("/tmp/", tmpFileName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to create temp file", err)
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	bytesWritten, err := io.Copy(tmpFile, videoFormFile)
	if err != nil {
		log.Println("failed to write video file")
		respondWithError(w, http.StatusInternalServerError, "internal server error", err)
		return
	}

	log.Printf("wrote %d bytes to %s\n", bytesWritten, tmpFileName)

	if _, err := tmpFile.Seek(0, io.SeekStart); err != nil {
		log.Println("failed to reset video file")
		respondWithError(w, http.StatusInternalServerError, "internal server error", err)
		return
	}

	aspectRatio, err := getVideoAspectRatio(tmpFile.Name())
	if err != nil {
		log.Fatal(err)
		respondWithError(w, http.StatusInternalServerError, "internal server error", err)
	}

	log.Printf("aspect ratio of %s = %s\n", tmpFile.Name(), aspectRatio)

	var bucketPrefix string
	switch aspectRatio {
	case "9:16":
		bucketPrefix = "portrait"
	case "16:9":
		bucketPrefix = "landscape"
	default:
		bucketPrefix = "other"
	}

	s3Key := fmt.Sprintf("%s-%s", bucketPrefix, getAssetPath(contentType))

	log.Printf("uploading %s to s3 bucket %s", s3Key, cfg.s3Bucket)

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &s3Key,
		Body:        tmpFile,
		ContentType: &contentType,
	})

	if err != nil {
		respondWithError(w, http.StatusConflict, "failed to upload video to s3 bucket", err)
		return
	}

	log.Printf("uploaded %s to s3 bucket %s\n", s3Key, cfg.s3Bucket)

	videoURL := cfg.getObjectURL(s3Key)
	videoMetadata.VideoURL = &videoURL

	err = cfg.db.UpdateVideo(videoMetadata)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMetadata)
}
