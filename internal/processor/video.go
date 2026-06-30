package processor

import (
	"encoding/binary"
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

	// Si el comprimido NO redujo tamaño, no nos quedamos con el re-encode.
	// Pero igual debemos garantizar que el archivo final tenga el moov atom
	// al inicio (faststart) para que el video arranque sin bajar todo.
	//   - Si el original YA está en faststart → devolver nil (usar original tal cual).
	//   - Si NO lo está y es remuxeable (mp4/mov, codecs compatibles) → remux
	//     stream-copy con faststart (casi instantáneo, sin re-encodar).
	//   - Si no es remuxeable o el remux falla → nil (degradar al original).
	if int64(len(data)) >= originalSize {
		ext := strings.ToLower(filepath.Ext(inputPath))
		remuxable := ext == ".mp4" || ext == ".mov" || ext == ".m4v"
		if remuxable && !hasFaststart(inputPath) {
			if res, err := remuxFaststartFull(inputPath, originalSize); err == nil {
				return res, nil
			}
			// remux falló (p.ej. codec no copiable a mp4): conservar original
		}
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

// RemuxFaststart reordena el moov atom al inicio SIN re-encodar (stream copy).
// Es casi instantáneo: solo reescribe el contenedor. Requiere que los codecs
// del origen sean compatibles con MP4 (h264/hevc/aac); si no, ffmpeg falla.
func RemuxFaststart(inputPath, outputPath string) error {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}

	args := []string{
		"-i", inputPath,
		"-c", "copy",
		"-movflags", "+faststart",
		"-y",
		outputPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg remux error: %s - %w", string(output), err)
	}
	return nil
}

// remuxFaststartFull aplica RemuxFaststart al original y devuelve el resultado
// listo para subir. Se usa cuando la compresión no redujo tamaño pero el video
// no está en faststart.
func remuxFaststartFull(inputPath string, originalSize int64) (*VideoCompressResult, error) {
	tmpOut, err := os.CreateTemp("", "faststart-*.mp4")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file: %w", err)
	}
	tmpOut.Close()
	defer os.Remove(tmpOut.Name())

	if err := RemuxFaststart(inputPath, tmpOut.Name()); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(tmpOut.Name())
	if err != nil {
		return nil, fmt.Errorf("error reading remuxed video: %w", err)
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

// hasFaststart escanea los atoms top-level de un MP4/MOV y reporta si el `moov`
// aparece antes del `mdat` (es decir, si ya está optimizado para streaming).
// Ante cualquier ambigüedad (error de lectura, contenedor raro) devuelve true
// para NO forzar un remux innecesario.
func hasFaststart(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	buf := make([]byte, 16)
	var offset int64

	// Límite de atoms top-level a inspeccionar (defensa contra archivos raros).
	for range 64 {
		if _, err := f.ReadAt(buf[:8], offset); err != nil {
			return true // EOF/error: no forzar remux
		}
		size := int64(binary.BigEndian.Uint32(buf[:4]))
		atom := string(buf[4:8])
		header := int64(8)

		if size == 1 {
			// tamaño de 64 bits en los 8 bytes siguientes
			if _, err := f.ReadAt(buf[:16], offset); err != nil {
				return true
			}
			size = int64(binary.BigEndian.Uint64(buf[8:16]))
			header = 16
		} else if size == 0 {
			// box se extiende hasta EOF: es el último
			return atom == "moov"
		}

		switch atom {
		case "moov":
			return true
		case "mdat":
			return false
		}

		if size < header {
			return true // tamaño inválido: no tocar
		}
		offset += size
	}
	return true
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
