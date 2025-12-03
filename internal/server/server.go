package server

import (
	"filestation/internal/auth"
	"filestation/internal/fileops"
	"filestation/internal/templates"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Port      int
	SiteTitle string
	UploadDir string
}

type Server struct {
	config    Config
	mux       *http.ServeMux
	auth      *auth.AuthManager
	templates *templates.TemplateManager
}

func New(config Config) *Server {
	tmpl, err := templates.New()
	if err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}

	s := &Server{
		config:    config,
		mux:       http.NewServeMux(),
		auth:      auth.New(),
		templates: tmpl,
	}
	s.routes()

	// Start cleanup task
	go s.cleanupTask()

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Static files (must be registered first, wrapped to only accept GET)
	fs := http.FileServer(http.Dir("static"))
	staticHandler := http.StripPrefix("/static/", fs)
	s.mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		staticHandler.ServeHTTP(w, r)
	})

	// Admin routes (more specific routes first)
	s.mux.HandleFunc("GET /admin/login", s.handleAdminLogin)
	s.mux.HandleFunc("POST /admin/login", s.handleAdminLoginPost)
	s.mux.HandleFunc("GET /admin/logout", s.handleAdminLogout)
	s.mux.HandleFunc("GET /admin/password", s.auth.Middleware(s.handleAdminPasswordPage))
	s.mux.HandleFunc("POST /admin/password", s.auth.Middleware(s.handleAdminPasswordPost))
	s.mux.HandleFunc("POST /admin/delete/{filename}", s.auth.Middleware(s.handleAdminDeleteFile))
	s.mux.HandleFunc("GET /admin", s.auth.Middleware(s.handleAdminDashboard))

	// Main routes
	s.mux.HandleFunc("GET /upload", s.handleUploadPage)
	s.mux.HandleFunc("POST /upload", s.handleUpload)
	s.mux.HandleFunc("GET /download/{filename}", s.handleDownload)
	s.mux.HandleFunc("POST /download/{filename}", s.handleDownloadPost)

	// Root route (must be registered last)
	s.mux.HandleFunc("GET /", s.handleIndex)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	files, err := fileops.GetFiles(s.config.UploadDir)
	if err != nil {
		http.Error(w, "Failed to list files", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"SiteTitle": s.config.SiteTitle,
		"TempFiles": files,
		"Now":       time.Now,
	}
	s.templates.Render(w, "index.html", data)
}

func (s *Server) handleUploadPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"SiteTitle": s.config.SiteTitle,
	}
	s.templates.Render(w, "upload.html", data)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// 10GB limit
	r.Body = http.MaxBytesReader(w, r.Body, 10<<30)
	if err := r.ParseMultipartForm(10 << 30); err != nil {
		http.Error(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	desc := r.FormValue("description")
	if desc == "" {
		desc = "上传者没有提供描述信息"
	}
	password := r.FormValue("password")
	expirationHours, _ := strconv.Atoi(r.FormValue("expiration"))
	if expirationHours == 0 {
		expirationHours = 24
	}

	meta := fileops.FileMetadata{
		Description:      desc,
		Uploader:         fileops.ClientInfo{IP: r.RemoteAddr, Device: r.UserAgent()}, // Simplified
		UploadTime:       time.Now(),
		ExpirationTime:   time.Now().Add(time.Duration(expirationHours) * time.Hour),
		OriginalFilename: header.Filename,
	}

	if password != "" {
		// In a real app, hash this. For now, store plain or hash here if auth package exposes it.
		// Let's assume we store the hash.
		// But wait, fileops expects hash string. We need a helper to hash.
		// For simplicity, let's just store it as is or skip password for now?
		// User asked for password protection.
		// We can use auth.HashPassword if we expose it.
		// For now, let's just leave it empty or implement a simple hash in fileops or auth.
		// Let's add HashPassword to auth.
		meta.PasswordHash = s.auth.HashPassword(password)
	}

	if err := fileops.SaveFile(file, header, s.config.UploadDir, meta); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"success": true, "message": "File uploaded successfully!"}`))
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	meta, err := fileops.GetFile(s.config.UploadDir, filename)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	if meta.HasPassword {
		data := map[string]interface{}{
			"SiteTitle":    s.config.SiteTitle,
			"Filename":     meta.OriginalFilename,
			"RealFilename": filename,
			"Error":        false,
		}
		s.templates.Render(w, "password.html", data)
		return
	}

	s.serveFile(w, r, filename, meta.OriginalFilename)
}

func (s *Server) handleDownloadPost(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	password := r.FormValue("password")

	meta, err := fileops.GetFile(s.config.UploadDir, filename)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	if !s.auth.CheckPassword(meta.PasswordHash, password) {
		data := map[string]interface{}{
			"SiteTitle":    s.config.SiteTitle,
			"Filename":     meta.OriginalFilename,
			"RealFilename": filename,
			"Error":        true,
		}
		s.templates.Render(w, "password.html", data)
		return
	}

	s.serveFile(w, r, filename, meta.OriginalFilename)
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, filename, originalName string) {
	path := filepath.Join(s.config.UploadDir, filename)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", originalName))
	http.ServeFile(w, r, path)
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	s.templates.Render(w, "admin/login.html", map[string]interface{}{"SiteTitle": s.config.SiteTitle})
}

func (s *Server) handleAdminLoginPost(w http.ResponseWriter, r *http.Request) {
	password := r.FormValue("password")
	if token, ok := s.auth.Login(password); ok {
		http.SetCookie(w, &http.Cookie{
			Name:  "session_token",
			Value: token,
			Path:  "/",
		})
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.templates.Render(w, "admin/login.html", map[string]interface{}{
		"SiteTitle": s.config.SiteTitle,
		"Error":     true,
	})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   "session_token",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	files, _ := fileops.GetFiles(s.config.UploadDir)
	s.templates.Render(w, "admin/dashboard.html", map[string]interface{}{
		"SiteTitle": s.config.SiteTitle,
		"Files":     files,
	})
}

func (s *Server) handleAdminPasswordPage(w http.ResponseWriter, r *http.Request) {
	s.templates.Render(w, "admin/change_password.html", map[string]interface{}{"SiteTitle": s.config.SiteTitle})
}

func (s *Server) handleAdminPasswordPost(w http.ResponseWriter, r *http.Request) {
	oldPass := r.FormValue("old_password")
	newPass := r.FormValue("new_password")

	if s.auth.ChangePassword(oldPass, newPass) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	} else {
		s.templates.Render(w, "admin/change_password.html", map[string]interface{}{
			"SiteTitle": s.config.SiteTitle,
			"Error":     true,
		})
	}
}

func (s *Server) handleAdminDeleteFile(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	os.Remove(fmt.Sprintf("%s/%s", s.config.UploadDir, filename))
	os.Remove(fmt.Sprintf("%s/.%s.json", s.config.UploadDir, filename))
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) cleanupTask() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		if err := fileops.Cleanup(s.config.UploadDir); err != nil {
			log.Printf("Error cleaning up files: %v", err)
		}
	}
}
