package fileops

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type FileMetadata struct {
	Description      string    `json:"description"`
	Uploader         ClientInfo `json:"uploader"`
	UploadTime       time.Time `json:"upload_time"`
	ExpirationTime   time.Time `json:"expiration_time"`
	OriginalFilename string    `json:"original_filename"`
	PasswordHash     string    `json:"password_hash,omitempty"`
	Filename         string    `json:"-"` // Internal use
	Size             int64     `json:"-"` // Internal use
	IsTemp           bool      `json:"-"` // Internal use
	RemainingTime    string    `json:"-"` // Internal use
	HasPassword      bool      `json:"-"` // Internal use
	FormattedSize    string    `json:"-"` // Internal use
	Icon             string    `json:"-"` // Internal use
}

type ClientInfo struct {
	IP     string `json:"ip"`
	Device string `json:"device"`
}

func SaveFile(file multipart.File, header *multipart.FileHeader, uploadDir string, meta FileMetadata) error {
	// Create unique filename
	uniqueFilename := fmt.Sprintf("%s_%s", uuidShort(), header.Filename)
	filepath := filepath.Join(uploadDir, uniqueFilename)
	
	// Save file
	dst, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return err
	}

	// Save metadata file with dot prefix (e.g., .filename.json)
	metaPath := filepathJoin(uploadDir, "."+uniqueFilename+".json")
	
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(metaPath, metaJSON, 0644)
}

func GetFiles(uploadDir string) ([]FileMetadata, error) {
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		return nil, err
	}

	var files []FileMetadata
	now := time.Now()

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasSuffix(entry.Name(), ".part") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		meta := FileMetadata{
			Filename:         entry.Name(),
			OriginalFilename: entry.Name(),
			Size:             info.Size(),
			UploadTime:       info.ModTime(),
			Description:      "临时文件",
			IsTemp:           true,
			FormattedSize:    formatSize(info.Size()),
			Icon:             getFileIcon(entry.Name()),
		}

		// Read metadata file
		metaPath := filepathJoin(uploadDir, "."+entry.Name()+".json")
		if metaData, err := os.ReadFile(metaPath); err == nil {
			var storedMeta FileMetadata
			if err := json.Unmarshal(metaData, &storedMeta); err == nil {
				meta.Description = storedMeta.Description
				meta.Uploader = storedMeta.Uploader
				meta.OriginalFilename = storedMeta.OriginalFilename
				meta.ExpirationTime = storedMeta.ExpirationTime
				meta.PasswordHash = storedMeta.PasswordHash
				meta.HasPassword = meta.PasswordHash != ""
				
				// Update icon based on original filename
				if meta.OriginalFilename != "" {
					meta.Icon = getFileIcon(meta.OriginalFilename)
				}
				
				if !meta.ExpirationTime.IsZero() {
					if meta.ExpirationTime.After(now) {
						meta.RemainingTime = formatDuration(meta.ExpirationTime.Sub(now))
					} else {
						// Expired, should be cleaned up, but skip for now
						continue
					}
				}
			}
		}

		files = append(files, meta)
	}

	// Sort by upload time desc
	sort.Slice(files, func(i, j int) bool {
		return files[i].UploadTime.After(files[j].UploadTime)
	})

	return files, nil
}

func GetFile(uploadDir, filename string) (*FileMetadata, error) {
	// Security check for path traversal
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return nil, fmt.Errorf("invalid filename")
	}

	filePath := filepath.Join(uploadDir, filename)
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	meta := &FileMetadata{
		Filename:         filename,
		OriginalFilename: filename,
		Size:             info.Size(),
		UploadTime:       info.ModTime(),
	}

	metaPath := filepathJoin(uploadDir, "."+filename+".json")
	if metaData, err := os.ReadFile(metaPath); err == nil {
		json.Unmarshal(metaData, meta)
		meta.HasPassword = meta.PasswordHash != ""
	}

	return meta, nil
}

// Helpers

func uuidShort() string {
	// Generate a short unique identifier based on current time
	return fmt.Sprintf("%x", time.Now().UnixNano())[:8]
}

func filepathJoin(dir, name string) string {
	return filepath.Join(dir, name)
}

func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

func getFileIcon(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	icons := map[string][]string{
		"file-zipper":     {".zip", ".rar", ".7z"},
		"box":             {".tar", ".xz", ".gz"},
		"file-pdf":        {".pdf"},
		"file-word":       {".doc", ".docx"},
		"file-excel":      {".xls", ".xlsx"},
		"file-powerpoint": {".ppt", ".pptx"},
		"file-lines":      {".txt"},
		"book":            {".md"},
		"file-image":      {".jpg", ".jpeg", ".png", ".gif", ".bmp"},
		"file-audio":      {".mp3", ".wav", ".m4a", ".aac", ".ogg", ".flac"},
		"file-video":      {".mp4", ".avi", ".mkv", ".mov", ".flv", ".wmv", ".webm"},
		"cube":            {".exe", ".bin", ".jar"},
		"file-code":       {".py", ".c", ".cpp", ".java", ".html", ".css", ".js", ".go"},
		"terminal":        {".sh", ".bat"},
		"database":        {".accdb", ".db", ".sql", ".sqlite"},
	}

	for icon, exts := range icons {
		for _, e := range exts {
			if e == ext {
				return icon
			}
		}
	}
	return "file"
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%d天%d小时", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%d小时%d分", hours, minutes)
	}
	return fmt.Sprintf("%d分钟", minutes)
}

// Cleanup removes expired files from the upload directory
func Cleanup(uploadDir string) error {
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		return err
	}

	now := time.Now()
	defaultRetention := 24 * time.Hour

	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasSuffix(entry.Name(), ".part") {
			continue
		}

		filePath := filepath.Join(uploadDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Check metadata file for expiration time
		metaPath := filepathJoin(uploadDir, "."+entry.Name()+".json")
		expirationTime := time.Time{}

		if metaData, err := os.ReadFile(metaPath); err == nil {
			var storedMeta FileMetadata
			if err := json.Unmarshal(metaData, &storedMeta); err == nil {
				if !storedMeta.ExpirationTime.IsZero() {
					expirationTime = storedMeta.ExpirationTime
				}
			}
		}

		// If no explicit expiration time, use file modification time + default retention
		if expirationTime.IsZero() {
			expirationTime = info.ModTime().Add(defaultRetention)
		}

		// Delete expired files
		if now.After(expirationTime) {
			os.Remove(filePath)
			os.Remove(metaPath)
		}
	}

	return nil
}