package processor

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StartTempCleaner limpia archivos temporales huérfanos cada N minutos
func StartTempCleaner(intervalMins int) {
	if intervalMins <= 0 {
		intervalMins = 5
	}

	go func() {
		for {
			time.Sleep(time.Duration(intervalMins) * time.Minute)
			cleaned := cleanTempFiles()
			if cleaned > 0 {
				log.Printf("🧹 Limpiados %d archivos temporales huerfanos", cleaned)
			}
		}
	}()
}

func cleanTempFiles() int {
	tmpDir := os.TempDir()
	count := 0
	maxAge := 10 * time.Minute

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return 0
	}

	now := time.Now()
	for _, entry := range entries {
		name := entry.Name()
		// Solo limpiar archivos que creamos nosotros
		if !strings.HasPrefix(name, "thumb-") &&
			!strings.HasPrefix(name, "video-") &&
			!strings.HasPrefix(name, "webp-in-") &&
			!strings.HasPrefix(name, "webp-out-") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if now.Sub(info.ModTime()) > maxAge {
			fullPath := filepath.Join(tmpDir, name)
			if os.Remove(fullPath) == nil {
				count++
			}
		}
	}

	return count
}
