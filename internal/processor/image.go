package processor

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/disintegration/imaging"
)

type ImagePreset struct {
	MaxWidth  int
	MaxHeight int
	Quality   int    // 1-100
	Format    string // jpeg, png, webp, original
	Fit       string // contain (respeta aspect ratio), cover (fuerza dimensiones)
}

var Presets = map[string]ImagePreset{
	"producto":  {MaxWidth: 1200, MaxHeight: 1200, Quality: 85, Format: "webp", Fit: "contain"},
	"thumbnail": {MaxWidth: 300, MaxHeight: 300, Quality: 75, Format: "webp", Fit: "contain"},
	"banner":    {MaxWidth: 1920, MaxHeight: 720, Quality: 80, Format: "webp", Fit: "contain"},
	"logo":      {MaxWidth: 512, MaxHeight: 512, Quality: 90, Format: "webp", Fit: "contain"},
	"profile":   {MaxWidth: 500, MaxHeight: 500, Quality: 85, Format: "webp", Fit: "contain"},
	"galeria":   {MaxWidth: 1600, MaxHeight: 1600, Quality: 82, Format: "webp", Fit: "contain"},
	"medium":    {MaxWidth: 800, MaxHeight: 800, Quality: 80, Format: "webp", Fit: "contain"},
	"default":   {MaxWidth: 1600, MaxHeight: 1600, Quality: 80, Format: "webp", Fit: "contain"},
}

type ImageResult struct {
	Data          []byte
	Width         int
	Height        int
	Format        string
	OriginalSize  int64
	ProcessedSize int64
	ContentType   string
}

func ProcessImage(data io.Reader, originalSize int64, presetName string, customWidth, customHeight, customQuality int, format string) (*ImageResult, error) {
	// Leer toda la data
	raw, err := io.ReadAll(data)
	if err != nil {
		return nil, fmt.Errorf("error reading image data: %w", err)
	}

	// Decodificar imagen
	img, originalFormat, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("error decoding image (%s): %w", originalFormat, err)
	}

	// Obtener preset
	preset, exists := Presets[presetName]
	if !exists {
		preset = Presets["default"]
	}

	// Aplicar custom overrides
	maxW := preset.MaxWidth
	maxH := preset.MaxHeight
	quality := preset.Quality
	outputFormat := preset.Format

	if customWidth > 0 {
		maxW = customWidth
	}
	if customHeight > 0 {
		maxH = customHeight
	}
	if customQuality > 0 && customQuality <= 100 {
		quality = customQuality
	}
	if format != "" {
		outputFormat = format
	}

	// Obtener dimensiones originales
	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()

	// Resize solo si la imagen es más grande que el target
	if origW > maxW || origH > maxH {
		if preset.Fit == "cover" {
			img = imaging.Fill(img, maxW, maxH, imaging.Center, imaging.Lanczos)
		} else {
			img = imaging.Fit(img, maxW, maxH, imaging.Lanczos)
		}
	}

	// Obtener nuevas dimensiones
	newBounds := img.Bounds()
	newW := newBounds.Dx()
	newH := newBounds.Dy()

	// Encode
	var buf bytes.Buffer
	var contentType string

	switch strings.ToLower(outputFormat) {
	case "webp":
		// Intentar WebP via cwebp CLI
		webpData, webpErr := encodeWebP(img, quality)
		if webpErr != nil {
			// Fallback a JPEG si cwebp no está disponible
			err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
			contentType = "image/jpeg"
			outputFormat = "jpeg"
		} else {
			buf = *bytes.NewBuffer(webpData)
			contentType = "image/webp"
		}
	case "png":
		err = png.Encode(&buf, img)
		contentType = "image/png"
	default: // jpeg
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
		contentType = "image/jpeg"
		outputFormat = "jpeg"
	}

	if err != nil {
		return nil, fmt.Errorf("error encoding image to %s: %w", outputFormat, err)
	}

	processedData := buf.Bytes()

	// Si el procesado es más grande que el original (raro pero posible con PNG pequeños),
	// devolver el original
	if int64(len(processedData)) >= originalSize && outputFormat != "webp" {
		return &ImageResult{
			Data:          raw,
			Width:         origW,
			Height:        origH,
			Format:        originalFormat,
			OriginalSize:  originalSize,
			ProcessedSize: originalSize,
			ContentType:   getContentType(originalFormat),
		}, nil
	}

	return &ImageResult{
		Data:          processedData,
		Width:         newW,
		Height:        newH,
		Format:        outputFormat,
		OriginalSize:  originalSize,
		ProcessedSize: int64(len(processedData)),
		ContentType:   contentType,
	}, nil
}

// encodeWebP usa cwebp CLI para convertir a WebP (disponible en Docker con libwebp)
func encodeWebP(img image.Image, quality int) ([]byte, error) {
	if _, err := exec.LookPath("cwebp"); err != nil {
		return nil, fmt.Errorf("cwebp not found")
	}

	// Escribir PNG temporal
	tmpIn, err := os.CreateTemp("", "webp-in-*.png")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpIn.Name())

	if err := png.Encode(tmpIn, img); err != nil {
		tmpIn.Close()
		return nil, err
	}
	tmpIn.Close()

	// Archivo de salida
	tmpOut, err := os.CreateTemp("", "webp-out-*.webp")
	if err != nil {
		return nil, err
	}
	tmpOut.Close()
	defer os.Remove(tmpOut.Name())

	// Ejecutar cwebp
	cmd := exec.Command("cwebp", "-q", fmt.Sprintf("%d", quality), tmpIn.Name(), "-o", tmpOut.Name())
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("cwebp error: %s - %w", string(output), err)
	}

	return os.ReadFile(tmpOut.Name())
}

func getContentType(format string) string {
	switch strings.ToLower(format) {
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	case "gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}
