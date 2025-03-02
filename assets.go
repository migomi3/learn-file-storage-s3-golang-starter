package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func getAssetPath(mediaType string) (string, error) {
	key := make([]byte, 32)
	_, err := rand.Read(key)
	if err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(key)

	ext := mediaTypeToExt(mediaType)
	return fmt.Sprintf("%s%s", id, ext), nil
}

func (cfg apiConfig) getAssetDiskPath(assetPath string) string {
	return filepath.Join(cfg.assetsRoot, assetPath)
}

func (cfg apiConfig) getAssetURL(assetPath string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, assetPath)
}

func (cfg apiConfig) getS3AssetURL(assetPath string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, assetPath)
}

func mediaTypeToExt(mediaType string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return ".bin"
	}
	return "." + parts[1]
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var buffer bytes.Buffer
	cmd.Stdout = &buffer

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	jsonData := struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}{}
	if err := json.Unmarshal(buffer.Bytes(), &jsonData); err != nil {
		return "", err
	}
	if len(jsonData.Streams) == 0 {
		return "other", errors.New("no streams found in ffprobe output")
	}

	width := jsonData.Streams[0].Width
	height := jsonData.Streams[0].Height

	if width == 16*height/9 {
		return "16:9", nil
	} else if height == 16*width/9 {
		return "9:16", nil
	}
	return "other", nil
}

func addVideoOrientationPrefix(key, filePath string) (string, error) {
	ratio, err := getVideoAspectRatio(filePath)
	if err != nil {
		return "", err
	}

	switch ratio {
	case "16:9":
		return fmt.Sprintf("landscape/%s", key), nil
	case "9:16":
		return fmt.Sprintf("portrait/%s", key), nil
	default:
		return fmt.Sprintf("other/%s", key), nil
	}
}

func processVideoForFastStart(filePath string) (string, error) {
	if _, err := os.Stat(filePath); err != nil {
		return "", err
	}

	processedFilePath := fmt.Sprintf("%s.processing", filePath)

	cmd := exec.Command(
		"ffmpeg", "-y",
		"-i", filePath,
		"-c", "copy",
		"-movflags", "faststart",
		"-f", "mp4", processedFilePath,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg error: %v, stderr: %s", err, stderr.String())
	}

	processedFile, err := os.Open(processedFilePath)
	if err != nil {
		return "", err
	}
	defer processedFile.Close()

	return processedFilePath, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	client := s3.NewPresignClient(s3Client)
	request, err := client.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}

	return request.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil || *video.VideoURL == "" {
		return video, nil
	}

	splitURL := strings.Split(*video.VideoURL, ",")
	if len(splitURL) != 2 {
		return video, nil
	}

	url, err := generatePresignedURL(cfg.s3Client, splitURL[0], splitURL[1], time.Duration(3600)*time.Second)
	if err != nil {
		return database.Video{}, err
	}

	video.VideoURL = &url

	return video, nil
}
