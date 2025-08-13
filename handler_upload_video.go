package main

import (
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	//1GB Upload limit
	const uploadLimit = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

	//Extract videoID from the URL path and parse it as a UUID
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	//Authenticate user to get a userID
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

	//Get the video metadata from the database
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video", err)
		return
	}

	//Check if the user is the video owner
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "User not authorized to update this video", err)
		return
	}

	//Parse the uploaded video from the form data
	file, handler, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	//Validating the file to make sure it is an MP4 video
	mediaType, _, err := mime.ParseMediaType(handler.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}

	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type, only MP4 is allowed", nil)
		return
	}

	//Saving uploaded file to a temporary file on disk
	videoFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unbale to create temp file", err)
		return
	}
	defer os.Remove(videoFile.Name())
	defer videoFile.Close()

	if _, err := io.Copy(videoFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error saving file", err)
		return
	}

	//This will reset the temp file (videoFile) pointer to the beginning to allow us
	//to read the file again from the beginning
	_, err = videoFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not reset file pointer", err)
		return
	}

	//Put object into S3
	key := getAssetPath(mediaType)

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(key),
		Body:        videoFile,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload to S3", err)
		return
	}

	videoURL := cfg.getObjectURL(key)
	video.VideoURL = &videoURL

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to update video", err)
	}

	respondWithJSON(w, http.StatusOK, video)

}
