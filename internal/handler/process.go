package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"media-processor/internal/processor"
	"media-processor/internal/queue"
	"media-processor/pkg/response"
)

type ProcessHandler struct {
	pool          *queue.WorkerPool
	maxImageMB    int
	maxVideoMB    int
}

func NewProcessHandler(pool *queue.WorkerPool, maxImageMB, maxVideoMB int) *ProcessHandler {
	return &ProcessHandler{pool: pool, maxImageMB: maxImageMB, maxVideoMB: maxVideoMB}
}

// POST /process/image
func (h *ProcessHandler) ProcessImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Solo POST")
		return
	}

	// Parse multipart (max size)
	maxBytes := int64(h.maxImageMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		response.Error(w, http.StatusBadRequest, "FILE_TOO_LARGE", fmt.Sprintf("Imagen excede %dMB", h.maxImageMB))
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "NO_FILE", "No se envio archivo")
		return
	}
	defer file.Close()

	log.Printf("📥 IMAGE | %s | %.1fKB | preset=%s", header.Filename, float64(header.Size)/1024, r.FormValue("preset"))

	// Validar tipo
	contentType := header.Header.Get("Content-Type")
	if !isImageType(contentType) {
		response.Error(w, http.StatusBadRequest, "INVALID_TYPE", "Solo se aceptan imagenes (JPEG, PNG, GIF, WebP)")
		return
	}

	// Leer params
	preset := r.FormValue("preset")
	if preset == "" {
		preset = "default"
	}
	customW, _ := strconv.Atoi(r.FormValue("width"))
	customH, _ := strconv.Atoi(r.FormValue("height"))
	customQ, _ := strconv.Atoi(r.FormValue("quality"))
	format := r.FormValue("format")

	// Leer archivo en buffer
	var buf bytes.Buffer
	written, err := io.Copy(&buf, file)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "READ_ERROR", "Error leyendo archivo")
		return
	}

	// Validar que sea una imagen real (magic bytes)
	if !isValidImage(buf.Bytes()) {
		log.Printf("❌ IMAGE | %s | archivo corrupto o no es imagen valida", header.Filename)
		response.Error(w, http.StatusBadRequest, "CORRUPT_FILE", "El archivo no es una imagen valida o esta corrupto")
		return
	}

	// Crear job
	resultCh := make(chan queue.JobResult, 1)
	job := queue.Job{
		ID:        fmt.Sprintf("img-%d", time.Now().UnixNano()),
		CreatedAt: time.Now(),
		ResultCh:  resultCh,
		Execute: func(ctx context.Context) (interface{}, error) {
			return processor.ProcessImage(
				bytes.NewReader(buf.Bytes()),
				written,
				preset,
				customW, customH, customQ,
				format,
			)
		},
	}

	// Encolar
	if err := h.pool.Submit(job); err != nil {
		response.Error(w, http.StatusServiceUnavailable, "QUEUE_FULL", err.Error())
		return
	}

	// Esperar resultado
	select {
	case result := <-resultCh:
		if result.Error != nil {
			response.Error(w, http.StatusInternalServerError, "PROCESS_ERROR", result.Error.Error())
			return
		}

		imgResult := result.Data.(*processor.ImageResult)

		// Calcular reducción
		reduction := float64(0)
		if imgResult.OriginalSize > 0 {
			reduction = (1 - float64(imgResult.ProcessedSize)/float64(imgResult.OriginalSize)) * 100
		}

		log.Printf("✅ IMAGE | %s | %s → %s | %dKB → %dKB (%.1f%% reduccion) | %dx%d | %s",
			header.Filename, preset, imgResult.Format,
			imgResult.OriginalSize/1024, imgResult.ProcessedSize/1024,
			reduction, imgResult.Width, imgResult.Height, result.Took)

		// Enviar imagen procesada como binary
		w.Header().Set("Content-Type", imgResult.ContentType)
		w.Header().Set("Content-Length", strconv.FormatInt(imgResult.ProcessedSize, 10))
		w.Header().Set("X-Original-Size", strconv.FormatInt(imgResult.OriginalSize, 10))
		w.Header().Set("X-Processed-Size", strconv.FormatInt(imgResult.ProcessedSize, 10))
		w.Header().Set("X-Reduction", fmt.Sprintf("%.1f%%", reduction))
		w.Header().Set("X-Width", strconv.Itoa(imgResult.Width))
		w.Header().Set("X-Height", strconv.Itoa(imgResult.Height))
		w.Header().Set("X-Format", imgResult.Format)
		w.Header().Set("X-Duration", result.Took.String())
		w.WriteHeader(http.StatusOK)
		w.Write(imgResult.Data)

	case <-time.After(60 * time.Second):
		response.Error(w, http.StatusGatewayTimeout, "TIMEOUT", "Procesamiento excedio el tiempo limite")
	}
}

// POST /process/video/thumbnail
func (h *ProcessHandler) ProcessVideoThumbnail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Solo POST")
		return
	}

	maxBytes := int64(h.maxVideoMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		response.Error(w, http.StatusBadRequest, "FILE_TOO_LARGE", fmt.Sprintf("Video excede %dMB", h.maxVideoMB))
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "NO_FILE", "No se envio archivo")
		return
	}
	defer file.Close()

	log.Printf("📥 VIDEO | %s | %.1fMB", header.Filename, float64(header.Size)/(1024*1024))

	contentType := header.Header.Get("Content-Type")
	if !isVideoType(contentType) {
		response.Error(w, http.StatusBadRequest, "INVALID_TYPE", "Solo se aceptan videos")
		return
	}

	width, _ := strconv.Atoi(r.FormValue("width"))
	height, _ := strconv.Atoi(r.FormValue("height"))
	if width == 0 {
		width = 480
	}
	if height == 0 {
		height = 360
	}

	// Guardar video en archivo temporal
	ext := processor.GetVideoExtension(contentType)
	tmpFile, err := os.CreateTemp("", "video-*"+ext)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "TEMP_ERROR", "Error creando archivo temporal")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, file); err != nil {
		tmpFile.Close()
		response.Error(w, http.StatusInternalServerError, "WRITE_ERROR", "Error escribiendo video temporal")
		return
	}
	tmpFile.Close()

	// Crear job
	resultCh := make(chan queue.JobResult, 1)
	job := queue.Job{
		ID:        fmt.Sprintf("vid-%d", time.Now().UnixNano()),
		CreatedAt: time.Now(),
		ResultCh:  resultCh,
		Execute: func(ctx context.Context) (interface{}, error) {
			return processor.ExtractVideoThumbnail(tmpPath, width, height)
		},
	}

	if err := h.pool.Submit(job); err != nil {
		response.Error(w, http.StatusServiceUnavailable, "QUEUE_FULL", err.Error())
		return
	}

	select {
	case result := <-resultCh:
		if result.Error != nil {
			response.Error(w, http.StatusInternalServerError, "PROCESS_ERROR", result.Error.Error())
			return
		}

		vidResult := result.Data.(*processor.VideoResult)

		log.Printf("✅ VIDEO THUMB | %s | %dx%d | duracion: %.1fs | %s",
			header.Filename, vidResult.Width, vidResult.Height, vidResult.Duration, result.Took)

		w.Header().Set("Content-Type", vidResult.ContentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(vidResult.ThumbnailData)))
		w.Header().Set("X-Video-Width", strconv.Itoa(vidResult.Width))
		w.Header().Set("X-Video-Height", strconv.Itoa(vidResult.Height))
		w.Header().Set("X-Video-Duration", fmt.Sprintf("%.1f", vidResult.Duration))
		w.Header().Set("X-Duration", result.Took.String())
		w.WriteHeader(http.StatusOK)
		w.Write(vidResult.ThumbnailData)

	case <-time.After(120 * time.Second):
		response.Error(w, http.StatusGatewayTimeout, "TIMEOUT", "Procesamiento de video excedio el tiempo limite")
	}
}

// POST /process/video
func (h *ProcessHandler) ProcessVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Solo POST")
		return
	}

	maxBytes := int64(h.maxVideoMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		response.Error(w, http.StatusBadRequest, "FILE_TOO_LARGE", fmt.Sprintf("Video excede %dMB", h.maxVideoMB))
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "NO_FILE", "No se envio archivo")
		return
	}
	defer file.Close()

	log.Printf("📥 VIDEO COMPRESS | %s | %.1fMB", header.Filename, float64(header.Size)/(1024*1024))

	contentType := header.Header.Get("Content-Type")
	if !isVideoType(contentType) {
		response.Error(w, http.StatusBadRequest, "INVALID_TYPE", "Solo se aceptan videos")
		return
	}

	maxWidth, _ := strconv.Atoi(r.FormValue("maxWidth"))
	if maxWidth == 0 {
		maxWidth = 1280
	}

	// Guardar video en archivo temporal
	ext := processor.GetVideoExtension(contentType)
	tmpFile, err := os.CreateTemp("", "video-in-*"+ext)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "TEMP_ERROR", "Error creando archivo temporal")
		return
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	written, err := io.Copy(tmpFile, file)
	if err != nil {
		tmpFile.Close()
		response.Error(w, http.StatusInternalServerError, "WRITE_ERROR", "Error escribiendo video temporal")
		return
	}
	tmpFile.Close()

	// Crear job
	resultCh := make(chan queue.JobResult, 1)
	job := queue.Job{
		ID:        fmt.Sprintf("vidcomp-%d", time.Now().UnixNano()),
		CreatedAt: time.Now(),
		ResultCh:  resultCh,
		Execute: func(ctx context.Context) (interface{}, error) {
			return processor.CompressVideoFull(tmpPath, written, maxWidth)
		},
	}

	if err := h.pool.Submit(job); err != nil {
		response.Error(w, http.StatusServiceUnavailable, "QUEUE_FULL", err.Error())
		return
	}

	select {
	case result := <-resultCh:
		if result.Error != nil {
			response.Error(w, http.StatusInternalServerError, "PROCESS_ERROR", result.Error.Error())
			return
		}

		vidResult := result.Data.(*processor.VideoCompressResult)

		// nil significa que el comprimido era más grande que el original
		if vidResult == nil {
			log.Printf("⏭️ VIDEO | %s | compresion no redujo tamaño, enviando original", header.Filename)
			// Enviar el original de vuelta
			origData, _ := os.ReadFile(tmpPath)
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("X-Skipped", "true")
			w.Header().Set("X-Duration", result.Took.String())
			w.WriteHeader(http.StatusOK)
			w.Write(origData)
			return
		}

		reduction := float64(0)
		if vidResult.OriginalSize > 0 {
			reduction = (1 - float64(vidResult.ProcessedSize)/float64(vidResult.OriginalSize)) * 100
		}

		log.Printf("✅ VIDEO | %s | %.1fMB → %.1fMB (%.1f%% reduccion) | %dx%d | %s",
			header.Filename,
			float64(vidResult.OriginalSize)/(1024*1024),
			float64(vidResult.ProcessedSize)/(1024*1024),
			reduction, vidResult.Width, vidResult.Height, result.Took)

		w.Header().Set("Content-Type", vidResult.ContentType)
		w.Header().Set("Content-Length", strconv.FormatInt(vidResult.ProcessedSize, 10))
		w.Header().Set("X-Original-Size", strconv.FormatInt(vidResult.OriginalSize, 10))
		w.Header().Set("X-Processed-Size", strconv.FormatInt(vidResult.ProcessedSize, 10))
		w.Header().Set("X-Reduction", fmt.Sprintf("%.1f%%", reduction))
		w.Header().Set("X-Width", strconv.Itoa(vidResult.Width))
		w.Header().Set("X-Height", strconv.Itoa(vidResult.Height))
		w.Header().Set("X-Video-Duration", fmt.Sprintf("%.1f", vidResult.Duration))
		w.Header().Set("X-Duration", result.Took.String())
		w.WriteHeader(http.StatusOK)
		w.Write(vidResult.Data)

	case <-time.After(300 * time.Second): // 5 min timeout para videos
		response.Error(w, http.StatusGatewayTimeout, "TIMEOUT", "Compresion de video excedio el tiempo limite")
	}
}

// GET /health
func (h *ProcessHandler) Health(w http.ResponseWriter, r *http.Request) {
	stats := h.pool.Stats()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"time":    time.Now().Format(time.RFC3339),
		"workers": stats,
	})
}

// GET /presets
func (h *ProcessHandler) ListPresets(w http.ResponseWriter, r *http.Request) {
	presets := make(map[string]interface{})
	for name, p := range processor.Presets {
		presets[name] = map[string]interface{}{
			"maxWidth":  p.MaxWidth,
			"maxHeight": p.MaxHeight,
			"quality":   p.Quality,
			"format":    p.Format,
			"fit":       p.Fit,
		}
	}
	response.JSON(w, http.StatusOK, presets)
}

func isImageType(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.HasPrefix(ct, "image/")
}

func isVideoType(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.HasPrefix(ct, "video/")
}

// isValidImage verifica los magic bytes del archivo para confirmar que es una imagen real
func isValidImage(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	// JPEG: FF D8 FF
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return true
	}
	// PNG: 89 50 4E 47
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return true
	}
	// GIF: 47 49 46
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return true
	}
	// WebP: RIFF....WEBP
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return true
	}
	// BMP: 42 4D
	if data[0] == 0x42 && data[1] == 0x4D {
		return true
	}
	return false
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}
