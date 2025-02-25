package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func getAssetPath(videoID string, mediaType string) string {
	ext := mediaTypeToExt(mediaType)
	return fmt.Sprintf("%s%s", videoID, ext)
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

	ratio := float64(jsonData.Streams[0].Width) / float64(jsonData.Streams[0].Height)

	switch {
	case ratio > 1.0:
		return "16:9", nil
	case ratio < 1.0:
		return "9:16", nil
	default:
		return "other", nil
	}
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
