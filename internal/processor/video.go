package processor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type VideoResult struct {
	ThumbnailData []byte
	ThumbnailW    int
	ThumbnailH    int
	Duration      float64 // seconds
	Width         int
	Height        int
	ContentType   string
}

// ExtractVideoThumbnail genera un thumbnail del video usando FFmpeg
func ExtractVideoThumbnail(inputPath string, width, height int) (*VideoResult, error) {
	// Verificar que ffmpeg esté disponible
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	// Crear archivo temporal para el thumbnail
	tmpFile, err := os.CreateTemp("", "thumb-*.webp")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	// Generar thumbnail: frame al segundo 1 (o 0 si es muy corto)
	scale := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", width, height)
	args := []string{
		"-i", inputPath,
		"-ss", "1",
		"-vframes", "1",
		"-vf", scale,
		"-c:v", "libwebp",
		"-quality", "80",
		"-y",
		tmpPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Intentar con segundo 0 si falla
		args[3] = "0"
		cmd = exec.Command("ffmpeg", args...)
		output, err = cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("ffmpeg error: %s - %w", string(output), err)
		}
	}

	// Leer thumbnail generado
	thumbData, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("error reading thumbnail: %w", err)
	}

	// Obtener metadata del video
	duration := getVideoDuration(inputPath)
	vw, vh := getVideoDimensions(inputPath)

	return &VideoResult{
		ThumbnailData: thumbData,
		ThumbnailW:    width,
		ThumbnailH:    height,
		Duration:      duration,
		Width:         vw,
		Height:        vh,
		ContentType:   "image/webp",
	}, nil
}

type VideoCompressResult struct {
	Data          []byte
	OriginalSize  int64
	ProcessedSize int64
	Width         int
	Height        int
	Duration      float64
	ContentType   string
}

// CompressVideo comprime un video reduciendo bitrate
func CompressVideo(inputPath, outputPath string, maxWidth int) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}

	// Scale solo si es necesario
	vf := fmt.Sprintf("scale='min(%d,iw)':'-2'", maxWidth)

	args := []string{
		"-i", inputPath,
		"-vf", vf,
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "28",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
		"-y",
		outputPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg compress error: %s - %w", string(output), err)
	}

	return nil
}

// CompressVideoFull comprime un video y devuelve el resultado
func CompressVideoFull(inputPath string, originalSize int64, maxWidth int) (*VideoCompressResult, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found: %w", err)
	}

	// Archivo de salida temporal
	tmpOut, err := os.CreateTemp("", "compressed-*.mp4")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %w", err)
	}
	tmpOut.Close()
	defer os.Remove(tmpOut.Name())

	// Comprimir
	if err := CompressVideo(inputPath, tmpOut.Name(), maxWidth); err != nil {
		return nil, err
	}

	// Leer resultado
	data, err := os.ReadFile(tmpOut.Name())
	if err != nil {
		return nil, fmt.Errorf("error reading compressed video: %w", err)
	}

	// Si el comprimido es más grande que el original, devolver nil (usar original)
	if int64(len(data)) >= originalSize {
		return nil, nil
	}

	duration := getVideoDuration(inputPath)
	w, h := getVideoDimensions(tmpOut.Name())

	return &VideoCompressResult{
		Data:          data,
		OriginalSize:  originalSize,
		ProcessedSize: int64(len(data)),
		Width:         w,
		Height:        h,
		Duration:      duration,
		ContentType:   "video/mp4",
	}, nil
}

func getVideoDuration(path string) float64 {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return d
}

func getVideoDimensions(path string) (int, int) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) != 2 {
		return 0, 0
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	return w, h
}

// GetVideoExtension retorna la extensión del archivo temporal
func GetVideoExtension(contentType string) string {
	switch contentType {
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/webm":
		return ".webm"
	case "video/x-msvideo":
		return ".avi"
	default:
		return filepath.Ext(contentType)
	}
}
