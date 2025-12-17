// snapgo.go - SnapGo: Simple Version Control System
// Versión 1.0 - Listo para producción
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Estructuras de datos
type SnapshotMeta struct {
	ID        string   `json:"id"`
	Timestamp string   `json:"timestamp"`
	Message   string   `json:"message"`
	Hash      string   `json:"hash"`
	FileCount int      `json:"file_count"`
	Files     []string `json:"files"`
}

type Index struct {
	Snapshots []SnapshotMeta `json:"snapshots"`
	Current   string         `json:"current"`
}

type Config struct {
	Version        string   `json:"version"`
	AutoIgnore     []string `json:"auto_ignore"`
	Compression    int      `json:"compression_level"`
	MaxSnapshots   int      `json:"max_snapshots"`
	ChunkSizeMB    int      `json:"chunk_size_mb"`
	UseDelta       bool     `json:"use_delta"`
	Aliases        bool     `json:"enable_aliases"`
	EnableTrash    bool     `json:"enable_trash"`
	GitMode        bool     `json:"git_mode"`
}

// Alias para comandos SnapGo
var commandAliases = map[string]string{
	"s":     "snapshot",
	"l":     "list",
	"sh":    "show",
	"r":     "restore",
	"d":     "diff",
	"st":    "status",
	"log":   "history",
	"c":     "clean",
	"b":     "branch",
	"sw":    "switch",
	"t":     "trash",
	"sync":  "git-sync",
	"save":  "git-save",
	"back":  "git-back",
	"share": "git-share",
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	cmd := os.Args[1]
	
	// Manejar versión
	if cmd == "version" || cmd == "--version" || cmd == "-v" {
		fmt.Println("SnapGo v1.0 - Simple Snapshot-based Version Control")
		fmt.Println("Copyright 2025 - SnapGo Project")
		return
	}
	
	// Encontrar automáticamente el repositorio SnapGo
	rootDir := findRepositoryRoot()
	if rootDir == "" {
		rootDir = "." // Usar directorio actual si no se encuentra
	}
	
	if alias, ok := commandAliases[cmd]; ok {
		cmd = alias
		os.Args[1] = alias
	}

	switch cmd {
	case "init":
		must(initRepo("."))
	case "snapshot":
		snapshotCmdWithRoot(rootDir)
	case "list":
		must(listSnapshots(rootDir))
	case "show":
		if len(os.Args) < 3 {
			fmt.Println("Uso: show <id>")
			return
		}
		must(showSnapshot(rootDir, os.Args[2]))
	case "restore":
		restoreCmdWithRoot(rootDir)
	case "diff":
		diffCmdWithRoot(rootDir)
	case "status":
		must(statusCmdWithRoot(rootDir))
	case "history":
		must(historyCmdWithRoot(rootDir))
	case "clean":
		must(cleanCmdWithRoot(rootDir))
	case "branch":
		branchCmdWithRoot(rootDir)
	case "switch":
		switchCmdWithRoot(rootDir)
	case "config":
		configCmdWithRoot(rootDir)
	case "trash":
		trashCmdWithRoot(rootDir)
	case "git-sync", "git-save", "git-back", "git-share":
		gitModeCmdWithRoot(cmd, rootDir)
	case "debug":
		// Comando de diagnóstico para debug
		must(debugRepo(rootDir))
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Printf("Comando desconocido: %s\n", cmd)
		fmt.Println("Usa 'snapgo help' para ver los comandos disponibles")
	}
}

func usage() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║                  S N A P G O  v1.0                    ║")
	fmt.Println("║         Simple Snapshot-based Version Control         ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("📦 Comandos básicos:")
	fmt.Println("  init                         Inicializar repositorio")
	fmt.Println("  snapshot -m <mensaje>        Crear snapshot (alias: s)")
	fmt.Println("  list                         Listar snapshots (alias: l)")
	fmt.Println("  show <id>                    Mostrar detalles (alias: sh)")
	fmt.Println("  restore <id> [--force]       Restaurar (alias: r)")
	fmt.Println("  diff <id1> <id2>             Comparar (alias: d)")
	fmt.Println()
	fmt.Println("🔧 Comandos avanzados:")
	fmt.Println("  status                       Ver estado actual (alias: st)")
	fmt.Println("  history                      Historial con formato (alias: log)")
	fmt.Println("  clean                        Limpiar snapshots viejos (alias: c)")
	fmt.Println("  branch [nombre]              Listar/crear ramas (alias: b)")
	fmt.Println("  switch <nombre>              Cambiar rama (alias: sw)")
	fmt.Println("  config                       Mostrar configuración")
	fmt.Println("  trash [list|empty|restore]   Gestionar papelera (alias: t)")
	fmt.Println()
	fmt.Println("🎯 Nombres especiales:")
	fmt.Println("  HEAD     Último snapshot")
	fmt.Println("  PREV     Anterior al último")
	fmt.Println()
	fmt.Println("ℹ️  Otros comandos:")
	fmt.Println("  debug                        Diagnóstico del repositorio")
	fmt.Println("  version                      Mostrar versión")
	fmt.Println("  help                         Mostrar esta ayuda")
	fmt.Println()
	fmt.Println("📚 Ejemplos:")
	fmt.Println("  snapgo init")
	fmt.Println("  snapgo snapshot -m \"Mi primer snapshot\"")
	fmt.Println("  snapgo list")
	fmt.Println("  snapgo diff HEAD PREV")
	fmt.Println("  snapgo restore 20251216-083025-82ea5cc2afc4")
}

func must(err error) {
	if err != nil {
		fmt.Println("❌ Error:", err)
		os.Exit(1)
	}
}

func repoPaths(root string) (snapgoDir, snapsDir, indexPath, configPath, ignorePath, trashDir string) {
	// Usar rutas absolutas para evitar confusiones
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	
	snapgoDir = filepath.Join(absRoot, ".snapgo")
	snapsDir = filepath.Join(snapgoDir, "snapshots")
	indexPath = filepath.Join(snapgoDir, "index.json")
	configPath = filepath.Join(snapgoDir, "config.json")
	ignorePath = filepath.Join(absRoot, ".snapgoignore")
	trashDir = filepath.Join(snapgoDir, "trash")
	return
}

// Función para encontrar automáticamente el repositorio SnapGo
func findRepositoryRoot() string {
	// Comenzar desde el directorio actual
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	
	// Buscar .snapgo en el directorio actual
	snapgoPath := filepath.Join(cwd, ".snapgo")
	if _, err := os.Stat(snapgoPath); err == nil {
		// Verificar que tenga la estructura correcta
		indexPath := filepath.Join(snapgoPath, "index.json")
		if _, err := os.Stat(indexPath); err == nil {
			return cwd
		}
	}
	
	// Buscar en subdirectorios comunes
	commonDirs := []string{"pruebas", "test", "tests", "sandbox", "demo", "src", "project"}
	for _, dir := range commonDirs {
		testPath := filepath.Join(cwd, dir, ".snapgo")
		if _, err := os.Stat(testPath); err == nil {
			indexPath := filepath.Join(testPath, "index.json")
			if _, err := os.Stat(indexPath); err == nil {
				return filepath.Join(cwd, dir)
			}
		}
	}
	
	// Buscar recursivamente en subdirectorios
	found := findRepoRecursive(cwd, 0, 3) // Profundidad máxima 3
	if found != "" {
		return found
	}
	
	return "" // No encontrado
}

// Función auxiliar para búsqueda recursiva
func findRepoRecursive(dir string, depth, maxDepth int) string {
	if depth >= maxDepth {
		return ""
	}
	
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			subdir := filepath.Join(dir, entry.Name())
			snapgoPath := filepath.Join(subdir, ".snapgo")
			
			if _, err := os.Stat(snapgoPath); err == nil {
				indexPath := filepath.Join(snapgoPath, "index.json")
				if _, err := os.Stat(indexPath); err == nil {
					return subdir
				}
			}
			
			// Buscar recursivamente
			if found := findRepoRecursive(subdir, depth+1, maxDepth); found != "" {
				return found
			}
		}
	}
	
	return ""
}

func initRepo(root string) error {
	snapgoDir, snapsDir, indexPath, configPath, ignorePath, trashDir := repoPaths(root)
	
	// Verificar si ya existe
	if _, err := os.Stat(indexPath); err == nil {
		// Ya existe, mostrar información
		var idx Index
		if err := readJSON(indexPath, &idx); err == nil {
			fmt.Printf("📦 Repositorio SnapGo ya existe aquí\n")
			fmt.Printf("📊 Snapshots existentes: %d\n", len(idx.Snapshots))
			if len(idx.Snapshots) > 0 {
				last := idx.Snapshots[len(idx.Snapshots)-1]
				fmt.Printf("🕒 Último snapshot: %s - %s\n", last.ID, last.Message)
			}
		}
		return nil
	}
	
	if err := os.MkdirAll(snapsDir, 0o755); err != nil {
		return err
	}
	
	if err := os.MkdirAll(trashDir, 0o755); err != nil {
		return err
	}
	
	idx := Index{
		Snapshots: []SnapshotMeta{},
		Current:   "main",
	}
	if err := writeJSON(indexPath, idx); err != nil {
		return err
	}
	
	config := Config{
		Version:      "1.0",
		AutoIgnore:   []string{"node_modules/", ".git/", "__pycache__/", ".snapgo/", "*.exe", "*.dll", "*.so", "*.dylib"},
		Compression:  6,
		MaxSnapshots: 100,
		ChunkSizeMB:  10,
		UseDelta:     false,
		Aliases:      true,
		EnableTrash:  true,
		GitMode:      false,
	}
	if err := writeJSON(configPath, config); err != nil {
		return err
	}
	
	if _, err := os.Stat(ignorePath); os.IsNotExist(err) {
		def := `# Archivos ignorados por SnapGo
# Directorios comunes
node_modules/
build/
dist/
.snapgo/
.vscode/
.idea/
__pycache__/
*.pyc

# Archivos binarios
*.exe
*.dll
*.so
*.dylib
*.bin

# Archivos de entorno
.env
.env.*
.secret*

# Logs y temporales
*.log
*.tmp
*.temp
*.cache

# Archivos del sistema
Thumbs.db
.DS_Store
desktop.ini

# Backup files
*.bak
*.backup
*~
`
		if err := os.WriteFile(ignorePath, []byte(def), 0o644); err != nil {
			return err
		}
	}
	
	fmt.Println("✅ Repositorio SnapGo inicializado en", snapgoDir)
	fmt.Println("💡 Usa 'snapgo snapshot -m \"mensaje\"' para crear tu primer snapshot")
	return nil
}

func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func loadConfig(root string) (Config, error) {
	_, _, _, configPath, _, _ := repoPaths(root)
	
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		config := Config{
			Version:      "1.0",
			AutoIgnore:   []string{"node_modules/", ".git/", "__pycache__/", ".snapgo/", "*.exe", "*.dll"},
			Compression:  6,
			MaxSnapshots: 100,
			ChunkSizeMB:  10,
			UseDelta:     false,
			Aliases:      true,
			EnableTrash:  true,
			GitMode:      false,
		}
		if err := writeJSON(configPath, config); err != nil {
			return Config{}, err
		}
		return config, nil
	}
	
	var config Config
	if err := readJSON(configPath, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func loadIgnore(root string) ([]string, error) {
	_, _, _, _, ignorePath, _ := repoPaths(root)
	
	data, err := os.ReadFile(ignorePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	
	lines := []string{}
	if err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			lines = append(lines, l)
		}
	}
	
	config, err := loadConfig(root)
	if err == nil {
		lines = append(lines, config.AutoIgnore...)
	}
	
	// Asegurar que .snapgo/ siempre esté ignorado
	lines = append(lines, ".snapgo/")
	
	return lines, nil
}

// Mejorar función isIgnored
func isIgnored(path string, patterns []string) bool {
	path = filepath.ToSlash(path)
	
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		
		p = filepath.ToSlash(p)
		
		// Manejar patrones que terminan con /
		if strings.HasSuffix(p, "/") {
			// Para directorios, verificar si el path comienza con el patrón
			if strings.HasPrefix(path, p) {
				return true
			}
			// También verificar si algún componente del path coincide
			pathParts := strings.Split(path, "/")
			for _, part := range pathParts {
				if part+"/" == p {
					return true
				}
			}
			continue
		}
		
		// Manejar patrones con wildcards
		if strings.Contains(p, "*") {
			// Intentar coincidencia con el nombre del archivo
			matched, _ := filepath.Match(p, filepath.Base(path))
			if matched {
				return true
			}
			// Intentar coincidencia con todo el path
			matched, _ = filepath.Match(p, path)
			if matched {
				return true
			}
			continue
		}
		
		// Coincidencia exacta del nombre del archivo
		if filepath.Base(path) == p {
			return true
		}
		
		// Coincidencia de sufijo (como .exe)
		if strings.HasPrefix(p, "*") {
			if strings.HasSuffix(path, p[1:]) {
				return true
			}
		}
		
		// Verificar si el path termina con el patrón
		if strings.HasSuffix(path, p) {
			return true
		}
	}
	
	return false
}

func collectFiles(root string, ignores []string) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		
		relUnix := filepath.ToSlash(rel)
		
		// Ignorar .snapgo/ explícitamente
		if strings.HasPrefix(relUnix, ".snapgo/") || relUnix == ".snapgo" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		
		if isIgnored(relUnix, ignores) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		
		if !d.IsDir() {
			files = append(files, relUnix)
		}
		return nil
	})
	
	sort.Strings(files)
	return files, err
}

// Nueva versión de snapshotCmd que acepta directorio raíz
func snapshotCmdWithRoot(rootDir string) {
	fs := flag.NewFlagSet("snapshot", flag.ExitOnError)
	msg := fs.String("m", "", "mensaje del snapshot")
	fs.Parse(os.Args[2:])
	
	if *msg == "" {
		fmt.Println("Uso: snapshot -m \"mensaje descriptivo\"")
		return
	}
	
	must(snapshot(rootDir, *msg))
}

func snapshot(root, message string) error {
	snapgoDir, snapsDir, indexPath, _, _, _ := repoPaths(root)
	if _, err := os.Stat(snapgoDir); os.IsNotExist(err) {
		if err := initRepo(root); err != nil {
			return err
		}
	}
	
	ignores, err := loadIgnore(root)
	if err != nil {
		return err
	}
	
	files, err := collectFiles(root, ignores)
	if err != nil {
		return err
	}
	
	if len(files) == 0 {
		return fmt.Errorf("no hay archivos para snapshot")
	}
	
	h := sha256.New()
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			return err
		}
		h.Write([]byte(f))
		h.Write(data)
	}
	sum := hex.EncodeToString(h.Sum(nil))[:12]
	
	id := time.Now().Format("20060102-150405") + "-" + sum
	archivePath := filepath.Join(snapsDir, id+".tar.gz")
	
	config, _ := loadConfig(root)
	if err := writeTarGz(root, archivePath, files, config.Compression); err != nil {
		return err
	}
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		return err
	}
	
	meta := SnapshotMeta{
		ID:        id,
		Timestamp: time.Now().Format(time.RFC3339),
		Message:   message,
		Hash:      sum,
		FileCount: len(files),
		Files:     files,
	}
	
	idx.Snapshots = append(idx.Snapshots, meta)
	
	config, _ = loadConfig(root)
	if config.MaxSnapshots > 0 && len(idx.Snapshots) > config.MaxSnapshots {
		oldest := idx.Snapshots[0]
		idx.Snapshots = idx.Snapshots[1:]
		
		oldPath := filepath.Join(snapsDir, oldest.ID+".tar.gz")
		os.Remove(oldPath)
	}
	
	if err := writeJSON(indexPath, idx); err != nil {
		return err
	}
	
	fmt.Printf("✅ Snapshot creado: %s\n", id)
	fmt.Printf("   📝 Mensaje: %s\n", message)
	fmt.Printf("   📁 Archivos: %d\n", len(files))
	
	return nil
}

func writeTarGz(root, out string, files []string, compression int) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	
	gw, err := gzip.NewWriterLevel(f, compression)
	if err != nil {
		return err
	}
	defer gw.Close()
	
	tw := tar.NewWriter(gw)
	defer tw.Close()
	
	for _, rel := range files {
		full := filepath.Join(root, rel)
		info, err := os.Stat(full)
		if err != nil {
			return err
		}
		
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		
		file, err := os.Open(full)
		if err != nil {
			return err
		}
		
		if _, err := io.Copy(tw, file); err != nil {
			file.Close()
			return err
		}
		file.Close()
	}
	
	return nil
}

func listSnapshots(root string) error {
	_, _, indexPath, _, _, _ := repoPaths(root)
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		// Mostrar error específico
		fmt.Printf("❌ No se pudo leer el índice en: %s\n", indexPath)
		fmt.Println("   ¿Estás en el directorio correcto?")
		fmt.Println("   Usa 'snapgo init' para crear un repositorio")
		return err
	}
	
	if len(idx.Snapshots) == 0 {
		fmt.Println("📭 No hay snapshots todavía.")
		fmt.Println("💡 Usa 'snapgo snapshot -m \"mensaje\"' para crear el primero.")
		return nil
	}
	
	fmt.Printf("📦 Snapshots disponibles (en %s):\n", root)
	for i, s := range idx.Snapshots {
		t, _ := time.Parse(time.RFC3339, s.Timestamp)
		timeStr := t.Format("02/01 15:04")
		
		prefix := "   "
		if i == len(idx.Snapshots)-1 {
			prefix = "🟢 "
		}
		
		fmt.Printf("%s%s  %s  %d archivos\n", prefix, s.ID, timeStr, s.FileCount)
		fmt.Printf("      \"%s\"\n", s.Message)
	}
	
	return nil
}

func showSnapshot(root, id string) error {
	id = resolveSpecialID(root, id)
	
	_, _, indexPath, _, _, _ := repoPaths(root)
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		return err
	}
	
	for _, s := range idx.Snapshots {
		if s.ID == id {
			fmt.Println("📊 Detalles del Snapshot")
			fmt.Println("══════════════════════════════════════════")
			fmt.Printf("🆔 ID:        %s\n", s.ID)
			
			t, _ := time.Parse(time.RFC3339, s.Timestamp)
			fmt.Printf("📅 Fecha:     %s\n", t.Format("02/01/2006 15:04:05"))
			fmt.Printf("🔒 Hash:      %s\n", s.Hash)
			fmt.Printf("📁 Archivos:  %d\n", s.FileCount)
			fmt.Printf("📝 Mensaje:   %s\n", s.Message)
			
			if len(s.Files) > 0 {
				fmt.Println("\n📄 Archivos incluidos:")
				for _, f := range s.Files {
					fmt.Printf("   • %s\n", f)
				}
			}
			
			return nil
		}
	}
	
	return fmt.Errorf("snapshot '%s' no encontrado", id)
}

// Nueva versión de restoreCmd que acepta directorio raíz
func restoreCmdWithRoot(rootDir string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	force := fs.Bool("force", false, "sobrescribir directorio actual")
	fs.Parse(os.Args[2:])
	
	if fs.NArg() < 1 {
		fmt.Println("Uso: restore <id> [--force]")
		return
	}
	
	id := fs.Arg(0)
	must(restore(rootDir, id, *force))
}

func restore(root, id string, force bool) error {
	id = resolveSpecialID(root, id)
	
	_, snapsDir, _, _, _, _ := repoPaths(root)
	
	archive := filepath.Join(snapsDir, id+".tar.gz")
	if _, err := os.Stat(archive); os.IsNotExist(err) {
		return fmt.Errorf("snapshot '%s' no encontrado", id)
	}
	
	if force {
		backupID := fmt.Sprintf("backup_pre_restore_%s", time.Now().Format("20060102_150405"))
		fmt.Printf("💾 Creando backup automático: %s\n", backupID)
		
		if err := snapshot(root, fmt.Sprintf("Backup antes de restaurar %s", id)); err != nil {
			return fmt.Errorf("error creando backup: %v", err)
		}
		
		if err := moveCurrentFilesToTrash(root, "pre_restore"); err != nil {
			fmt.Printf("⚠️  No se pudieron mover archivos a papelera: %v\n", err)
		}
	}
	
	target := root
	if !force {
		target = filepath.Join(root, "_restore_"+id)
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
	}
	
	if err := extractTarGz(archive, target); err != nil {
		return err
	}
	
	if force {
		fmt.Printf("✅ Snapshot '%s' restaurado en directorio actual\n", id)
		fmt.Println("   📝 Nota: Se creó un backup automático antes de la restauración")
		fmt.Println("   🗑️  Los archivos anteriores fueron movidos a la papelera (.snapgo/trash)")
	} else {
		fmt.Printf("✅ Snapshot '%s' restaurado en: %s\n", id, target)
	}
	
	return nil
}

func moveCurrentFilesToTrash(root, reason string) error {
	_, _, _, _, _, trashDir := repoPaths(root)
	
	config, err := loadConfig(root)
	if err != nil || !config.EnableTrash {
		return nil
	}
	
	trashSubdir := filepath.Join(trashDir, fmt.Sprintf("%s_%s", 
		time.Now().Format("20060102_150405"), reason))
	
	if err := os.MkdirAll(trashSubdir, 0o755); err != nil {
		return err
	}
	
	ignores, _ := loadIgnore(root)
	currentFiles, err := collectFiles(root, ignores)
	if err != nil {
		return err
	}
	
	movedCount := 0
	for _, file := range currentFiles {
		src := filepath.Join(root, file)
		dst := filepath.Join(trashSubdir, file)
		
		dstDir := filepath.Dir(dst)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			continue
		}
		
		if err := os.Rename(src, dst); err == nil {
			movedCount++
		}
	}
	
	if movedCount > 0 {
		fmt.Printf("📦 %d archivos movidos a papelera: %s\n", movedCount, trashSubdir)
	}
	
	return nil
}

func extractTarGz(archive, target string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	
	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gr.Close()
	
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		
		outPath := filepath.Join(target, hdr.Name)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		
		out, err := os.Create(outPath)
		if err != nil {
			return err
		}
		
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
	}
	
	return nil
}

// Nueva versión de diffCmd que acepta directorio raíz
func diffCmdWithRoot(rootDir string) {
	if len(os.Args) < 4 {
		fmt.Println("Uso: diff <id1> <id2>")
		fmt.Println("Ejemplo: diff HEAD PREV")
		fmt.Println("Nota: Necesitas al menos 2 snapshots para comparar")
		return
	}
	
	id1 := os.Args[2]
	id2 := os.Args[3]
	must(diffSnapshots(rootDir, id1, id2))
}

func diffSnapshots(root, id1, id2 string) error {
	id1 = resolveSpecialID(root, id1)
	id2 = resolveSpecialID(root, id2)
	
	if id1 == id2 {
		fmt.Println("ℹ️  Ambos snapshots son el mismo:")
		fmt.Printf("   🆔 ID: %s\n", id1)
		fmt.Println("   📊 Resultado: No hay diferencias")
		return nil
	}
	
	_, _, indexPath, _, _, _ := repoPaths(root)
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		return fmt.Errorf("error leyendo índice: %v", err)
	}
	
	if len(idx.Snapshots) == 0 {
		return fmt.Errorf("no hay snapshots disponibles")
	}
	
	if len(idx.Snapshots) == 1 {
		fmt.Println("ℹ️  Solo hay 1 snapshot disponible:")
		fmt.Printf("   🆔 ID: %s\n", idx.Snapshots[0].ID)
		fmt.Printf("   📝 Mensaje: %s\n", idx.Snapshots[0].Message)
		fmt.Println("   💡 Crea otro snapshot para poder comparar")
		return nil
	}
	
	var snap1, snap2 *SnapshotMeta
	for _, s := range idx.Snapshots {
		if s.ID == id1 {
			snap1 = &s
		}
		if s.ID == id2 {
			snap2 = &s
		}
	}
	
	if snap1 == nil {
		return fmt.Errorf("snapshot '%s' no encontrado", id1)
	}
	if snap2 == nil {
		return fmt.Errorf("snapshot '%s' no encontrado", id2)
	}
	
	var older, newer *SnapshotMeta
	time1, err1 := time.Parse(time.RFC3339, snap1.Timestamp)
	time2, err2 := time.Parse(time.RFC3339, snap2.Timestamp)
	
	if err1 != nil || err2 != nil {
		for i, s := range idx.Snapshots {
			if s.ID == id1 {
				older = snap1
				newer = snap2
				if i > 0 && idx.Snapshots[i-1].ID == id2 {
					older = snap2
					newer = snap1
				}
				break
			}
		}
	} else if time1.Before(time2) {
		older = snap1
		newer = snap2
	} else {
		older = snap2
		newer = snap1
	}
	
	setOlder := make(map[string]bool)
	setNewer := make(map[string]bool)
	
	for _, f := range older.Files {
		setOlder[f] = true
	}
	for _, f := range newer.Files {
		setNewer[f] = true
	}
	
	added := []string{}
	removed := []string{}
	
	for f := range setNewer {
		if !setOlder[f] {
			added = append(added, f)
		}
	}
	
	for f := range setOlder {
		if !setNewer[f] {
			removed = append(removed, f)
		}
	}
	
	fmt.Printf("📊 Comparación: %s → %s\n", older.ID, newer.ID)
	fmt.Printf("📅 Fecha: %s → %s\n", 
		formatTime(older.Timestamp), 
		formatTime(newer.Timestamp))
	fmt.Printf("📝 Mensajes: \"%s\" → \"%s\"\n",
		older.Message, newer.Message)
	
	if len(added) > 0 {
		fmt.Println("\n➕ Archivos añadidos:")
		for _, f := range added {
			fmt.Printf("   • %s\n", f)
		}
	}
	
	if len(removed) > 0 {
		fmt.Println("\n➖ Archivos eliminados:")
		for _, f := range removed {
			fmt.Printf("   • %s\n", f)
		}
	}
	
	commonFiles := []string{}
	for f := range setOlder {
		if setNewer[f] {
			commonFiles = append(commonFiles, f)
		}
	}
	
	if len(commonFiles) > 0 && (len(added) > 0 || len(removed) > 0) {
		fmt.Printf("\n🔸 %d archivos en ambos snapshots (podrían estar modificados)\n", len(commonFiles))
	}
	
	if len(added) == 0 && len(removed) == 0 {
		fmt.Println("\n✅ No hay diferencias en la lista de archivos")
	}
	
	return nil
}

// Nueva versión de statusCmd que acepta directorio raíz
func statusCmdWithRoot(root string) error {
	_, _, indexPath, _, _, _ := repoPaths(root)
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		fmt.Println("❌ No es un repositorio SnapGo")
		fmt.Println("💡 Usa 'snapgo init' para crear uno")
		return nil
	}
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		return err
	}
	
	fmt.Printf("📊 Estado del Repositorio (en %s)\n", root)
	fmt.Println("══════════════════════════════════════════")
	
	if len(idx.Snapshots) == 0 {
		fmt.Println("📭 No hay snapshots todavía")
	} else {
		last := idx.Snapshots[len(idx.Snapshots)-1]
		t, _ := time.Parse(time.RFC3339, last.Timestamp)
		fmt.Printf("🕒 Último snapshot: %s (%s)\n", last.ID, t.Format("02/01 15:04"))
		fmt.Printf("📝 Mensaje: %s\n", last.Message)
	}
	
	ignores, err := loadIgnore(root)
	if err != nil {
		return err
	}
	
	currentFiles, err := collectFiles(root, ignores)
	if err != nil {
		return err
	}
	
	if len(idx.Snapshots) > 0 {
		lastFiles := idx.Snapshots[len(idx.Snapshots)-1].Files
		setLast := make(map[string]bool)
		for _, f := range lastFiles {
			setLast[f] = true
		}
		
		newFiles := []string{}
		
		for _, f := range currentFiles {
			if !setLast[f] {
				newFiles = append(newFiles, f)
			}
		}
		
		if len(newFiles) > 0 {
			fmt.Println("\n🆕 Archivos nuevos no versionados:")
			for _, f := range newFiles {
				fmt.Printf("   • %s\n", f)
			}
		} else {
			fmt.Println("\n✅ No hay archivos nuevos")
		}
	} else {
		fmt.Printf("\n🆕 Archivos listos para el primer snapshot: %d\n", len(currentFiles))
		if len(currentFiles) > 0 && len(currentFiles) <= 10 {
			for _, f := range currentFiles {
				fmt.Printf("   • %s\n", f)
			}
		} else if len(currentFiles) > 10 {
			fmt.Printf("   (mostrando 10 de %d)\n", len(currentFiles))
			for i := 0; i < 10 && i < len(currentFiles); i++ {
				fmt.Printf("   • %s\n", currentFiles[i])
			}
		}
	}
	
	fmt.Println("\n💡 Usa 'snapgo snapshot -m \"mensaje\"' para guardar cambios")
	return nil
}

// Nueva versión de historyCmd que acepta directorio raíz
func historyCmdWithRoot(root string) error {
	_, _, indexPath, _, _, _ := repoPaths(root)
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		return err
	}
	
	if len(idx.Snapshots) == 0 {
		fmt.Println("📭 No hay historial de snapshots")
		return nil
	}
	
	fmt.Printf("📜 Historial de Snapshots (en %s)\n", root)
	fmt.Println("══════════════════════════════════════════")
	
	for i := len(idx.Snapshots) - 1; i >= 0; i-- {
		s := idx.Snapshots[i]
		t, _ := time.Parse(time.RFC3339, s.Timestamp)
		
		now := time.Now()
		diff := now.Sub(t)
		
		var timeStr string
		if diff < time.Hour {
			timeStr = "hace unos minutos"
		} else if diff < 24*time.Hour {
			hours := int(diff.Hours())
			timeStr = fmt.Sprintf("hace %d hora%s", hours, plural(hours))
		} else if diff < 7*24*time.Hour {
			days := int(diff.Hours() / 24)
			timeStr = fmt.Sprintf("hace %d día%s", days, plural(days))
		} else {
			timeStr = t.Format("02 Jan 2006")
		}
		
		fmt.Printf("\n🆔 [%s]\n", s.ID)
		fmt.Printf("   📅 %s | 📁 %d archivos\n", timeStr, s.FileCount)
		fmt.Printf("   📝 %s\n", s.Message)
		
		if i > 0 {
			fmt.Println("   ──────────────────────────────────────")
		}
	}
	
	return nil
}

// Nueva versión de cleanCmd que acepta directorio raíz
func cleanCmdWithRoot(root string) error {
	config, err := loadConfig(root)
	if err != nil {
		return err
	}
	
	_, _, indexPath, _, _, _ := repoPaths(root)
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		return err
	}
	
	if len(idx.Snapshots) <= config.MaxSnapshots {
		fmt.Printf("✅ Ya tienes %d snapshots (límite: %d)\n", len(idx.Snapshots), config.MaxSnapshots)
		return nil
	}
	
	toRemove := len(idx.Snapshots) - config.MaxSnapshots
	fmt.Printf("🧹 Limpiando %d snapshot(s) antiguo(s)...\n", toRemove)
	
	removed := 0
	_, snapsDir, _, _, _, _ := repoPaths(root)
	
	for i := 0; i < toRemove && i < len(idx.Snapshots); i++ {
		s := idx.Snapshots[i]
		archive := filepath.Join(snapsDir, s.ID+".tar.gz")
		
		if err := os.Remove(archive); err == nil {
			fmt.Printf("   🗑️  Eliminado: %s\n", s.ID)
			removed++
		}
	}
	
	if removed > 0 {
		idx.Snapshots = idx.Snapshots[removed:]
		if err := writeJSON(indexPath, idx); err != nil {
			return err
		}
	}
	
	fmt.Printf("✅ Limpieza completada. %d snapshots eliminados.\n", removed)
	return nil
}

// Nueva versión de branchCmd que acepta directorio raíz
func branchCmdWithRoot(rootDir string) {
	if len(os.Args) < 3 {
		listBranchesWithRoot(rootDir)
		return
	}
	
	branchName := os.Args[2]
	must(createBranch(rootDir, branchName))
}

func listBranchesWithRoot(root string) {
	_, _, indexPath, _, _, _ := repoPaths(root)
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		fmt.Println("Error cargando ramas:", err)
		return
	}
	
	fmt.Println("🌿 Ramas disponibles:")
	fmt.Printf("   🟢 %s (actual)\n", idx.Current)
	
	fmt.Println("\n💡 Usa 'snapgo branch <nombre>' para crear una nueva rama")
}

func createBranch(root, name string) error {
	if name == "" {
		return fmt.Errorf("nombre de rama no puede estar vacío")
	}
	
	_, _, indexPath, _, _, _ := repoPaths(root)
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		return err
	}
	
	idx.Current = name
	if err := writeJSON(indexPath, idx); err != nil {
		return err
	}
	
	fmt.Printf("✅ Rama '%s' creada y seleccionada\n", name)
	return nil
}

// Nueva versión de switchCmd que acepta directorio raíz
func switchCmdWithRoot(rootDir string) {
	if len(os.Args) < 3 {
		fmt.Println("Uso: switch <nombre-de-rama>")
		return
	}
	
	branchName := os.Args[2]
	must(switchBranch(rootDir, branchName))
}

func switchBranch(root, name string) error {
	_, _, indexPath, _, _, _ := repoPaths(root)
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		return err
	}
	
	oldBranch := idx.Current
	idx.Current = name
	
	if err := writeJSON(indexPath, idx); err != nil {
		return err
	}
	
	fmt.Printf("✅ Cambiado de '%s' a '%s'\n", oldBranch, name)
	return nil
}

// Nueva versión de configCmd que acepta directorio raíz
func configCmdWithRoot(root string) {
	config, err := loadConfig(root)
	if err != nil {
		fmt.Println("Error cargando configuración:", err)
		return
	}
	
	fmt.Printf("⚙️  Configuración de SnapGo (en %s)\n", root)
	fmt.Println("══════════════════════════════════════════")
	
	fmt.Printf("📦 Versión:          %s\n", config.Version)
	fmt.Printf("🗜️  Compresión:       nivel %d\n", config.Compression)
	fmt.Printf("🎯 Límite snapshots: %d\n", config.MaxSnapshots)
	fmt.Printf("📏 Tamaño chunk:     %d MB\n", config.ChunkSizeMB)
	fmt.Printf("🌀 Delta storage:    %v\n", config.UseDelta)
	fmt.Printf("🔤 Alias habilitados: %v\n", config.Aliases)
	fmt.Printf("🗑️  Papelera habilitada: %v\n", config.EnableTrash)
	fmt.Printf("🐱 Modo Git habilitado: %v\n", config.GitMode)
	
	fmt.Println("\n🚫 Auto-ignore:")
	for _, pattern := range config.AutoIgnore {
		fmt.Printf("   • %s\n", pattern)
	}
	
	fmt.Println("\n💡 Edita .snapgo/config.json para cambiar la configuración")
}

// Nueva versión de trashCmd que acepta directorio raíz
func trashCmdWithRoot(rootDir string) {
	if len(os.Args) < 3 {
		listTrashWithRoot(rootDir)
		return
	}
	
	subcmd := os.Args[2]
	switch subcmd {
	case "list":
		must(listTrashWithRoot(rootDir))
	case "empty":
		must(emptyTrash(rootDir))
	case "restore":
		if len(os.Args) < 4 {
			fmt.Println("Uso: trash restore <timestamp>")
			return
		}
		timestamp := os.Args[3]
		must(restoreFromTrash(rootDir, timestamp))
	default:
		fmt.Println("🗑️  Comandos de papelera:")
		fmt.Println("  trash list         Listar contenido de la papelera")
		fmt.Println("  trash empty        Vaciar la papelera")
		fmt.Println("  trash restore <ts> Restaurar archivos de un timestamp")
	}
}

func listTrashWithRoot(root string) error {
	_, _, _, _, _, trashDir := repoPaths(root)
	
	if _, err := os.Stat(trashDir); os.IsNotExist(err) {
		fmt.Println("🗑️  La papelera está vacía")
		return nil
	}
	
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		return err
	}
	
	if len(entries) == 0 {
		fmt.Println("🗑️  La papelera está vacía")
		return nil
	}
	
	fmt.Println("🗑️  Contenido de la Papelera")
	fmt.Println("══════════════════════════════════════════")
	for _, entry := range entries {
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			
			trashPath := filepath.Join(trashDir, entry.Name())
			files, _ := countFilesInDir(trashPath)
			
			fmt.Printf("📦 [%s]\n", entry.Name())
			fmt.Printf("   📁 Archivos: %d\n", files)
			fmt.Printf("   📅 Fecha: %s\n", info.ModTime().Format("02/01/2006 15:04:05"))
			fmt.Println()
		}
	}
	
	fmt.Println("💡 Usa 'snapgo trash restore <timestamp>' para restaurar archivos")
	return nil
}

func countFilesInDir(dir string) (int, error) {
	count := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}

func emptyTrash(root string) error {
	_, _, _, _, _, trashDir := repoPaths(root)
	
	if _, err := os.Stat(trashDir); os.IsNotExist(err) {
		fmt.Println("🗑️  La papelera ya está vacía")
		return nil
	}
	
	fmt.Print("¿Estás seguro de vaciar la papelera? (s/n): ")
	var response string
	fmt.Scanln(&response)
	
	if strings.ToLower(response) != "s" {
		fmt.Println("❌ Operación cancelada")
		return nil
	}
	
	err := os.RemoveAll(trashDir)
	if err != nil {
		return err
	}
	
	os.MkdirAll(trashDir, 0o755)
	
	fmt.Println("✅ Papelera vaciada correctamente")
	return nil
}

func restoreFromTrash(root, timestamp string) error {
	_, _, _, _, _, trashDir := repoPaths(root)
	
	trashPath := filepath.Join(trashDir, timestamp)
	if _, err := os.Stat(trashPath); os.IsNotExist(err) {
		return fmt.Errorf("no se encontró el timestamp '%s' en la papelera", timestamp)
	}
	
	fmt.Printf("🔄 Restaurando archivos desde: %s\n", timestamp)
	
	restored := 0
	err := filepath.WalkDir(trashPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		if d.IsDir() {
			return nil
		}
		
		rel, _ := filepath.Rel(trashPath, path)
		dst := filepath.Join(root, rel)
		
		dstDir := filepath.Dir(dst)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
		
		if err := os.Rename(path, dst); err == nil {
			restored++
			fmt.Printf("   ✅ Restaurado: %s\n", rel)
		}
		
		return nil
	})
	
	if err != nil {
		return err
	}
	
	fmt.Printf("✅ %d archivos restaurados desde la papelera\n", restored)
	
	os.RemoveAll(trashPath)
	
	return nil
}

// Nueva versión de gitModeCmd que acepta directorio raíz
func gitModeCmdWithRoot(cmd, root string) {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Println("❌ Git no está instalado o no está en el PATH")
		fmt.Println("   Instala Git o desactiva el modo Git en la configuración")
		return
	}
	
	config, err := loadConfig(root)
	if err != nil {
		fmt.Println("❌ No se pudo cargar la configuración")
		return
	}
	
	if !config.GitMode {
		fmt.Println("❌ Modo Git no está activado")
		fmt.Println("   Actívalo en .snapgo/config.json con \"git_mode\": true")
		return
	}
	
	switch cmd {
	case "git-sync":
		runGitCommand("pull origin main")
	case "git-save":
		if len(os.Args) < 3 {
			fmt.Println("Uso: save \"mensaje\"")
			return
		}
		message := os.Args[2]
		runGitCommand(fmt.Sprintf("commit -am \"%s\"", message))
	case "git-back":
		if len(os.Args) < 3 {
			fmt.Println("Uso: back <id>")
			return
		}
		id := os.Args[2]
		runGitCommand(fmt.Sprintf("checkout %s", id))
	case "git-share":
		runGitCommand("push origin main")
	}
}

func runGitCommand(args string) {
	fmt.Printf("🐱 [GIT] Ejecutando: git %s\n", args)
	
	cmdArgs := strings.Split(args, " ")
	cmd := exec.Command("git", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	
	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ Comando Git falló: %v\n", err)
	}
}

func resolveSpecialID(root, id string) string {
	_, _, indexPath, _, _, _ := repoPaths(root)
	
	var idx Index
	if err := readJSON(indexPath, &idx); err != nil {
		return id
	}
	
	if len(idx.Snapshots) == 0 {
		return id
	}
	
	if id == "HEAD" {
		return idx.Snapshots[len(idx.Snapshots)-1].ID
	} else if id == "PREV" {
		if len(idx.Snapshots) > 1 {
			return idx.Snapshots[len(idx.Snapshots)-2].ID
		} else {
			fmt.Println("ℹ️  Solo hay 1 snapshot, usando HEAD para PREV")
			return idx.Snapshots[0].ID
		}
	}
	
	return id
}

// Función de diagnóstico para debug
func debugRepo(root string) error {
	snapgoDir, snapsDir, indexPath, configPath, ignorePath, trashDir := repoPaths(root)
	
	fmt.Println("🔍 DIAGNÓSTICO DEL REPOSITORIO")
	fmt.Println("══════════════════════════════════════════")
	fmt.Printf("📁 Repositorio raíz: %s\n", root)
	fmt.Printf("📦 Directorio .snapgo: %s\n", snapgoDir)
	fmt.Printf("✅ Existe .snapgo: %v\n", fileExists(snapgoDir))
	fmt.Printf("✅ Existe índice: %v\n", fileExists(indexPath))
	fmt.Printf("✅ Existe snapshots: %v\n", fileExists(snapsDir))
	fmt.Printf("✅ Existe config: %v\n", fileExists(configPath))
	fmt.Printf("✅ Existe .snapgoignore: %v\n", fileExists(ignorePath))
	fmt.Printf("✅ Existe trash: %v\n", fileExists(trashDir))
	
	if fileExists(snapgoDir) {
		fmt.Println("\n📂 Contenido de .snapgo:")
		entries, err := os.ReadDir(snapgoDir)
		if err != nil {
			fmt.Printf("   ❌ Error leyendo: %v\n", err)
		} else {
			for _, entry := range entries {
				fmt.Printf("   • %s", entry.Name())
				if entry.IsDir() {
					fmt.Printf(" (directorio)")
					
					if entry.Name() == "snapshots" {
						snapPath := filepath.Join(snapgoDir, "snapshots")
						snapEntries, _ := os.ReadDir(snapPath)
						tarCount := 0
						for _, snap := range snapEntries {
							if strings.HasSuffix(snap.Name(), ".tar.gz") {
								tarCount++
							}
						}
						fmt.Printf(" - %d archivos .tar.gz", tarCount)
					}
				}
				fmt.Println()
			}
		}
	}
	
	if fileExists(indexPath) {
		fmt.Println("\n📊 Contenido del índice (index.json):")
		var idx Index
		if err := readJSON(indexPath, &idx); err != nil {
			fmt.Printf("   ❌ Error leyendo índice: %v\n", err)
		} else {
			fmt.Printf("   📦 Snapshots en índice: %d\n", len(idx.Snapshots))
			fmt.Printf("   🌿 Rama actual: %s\n", idx.Current)
			
			if len(idx.Snapshots) > 0 {
				fmt.Println("\n   📋 Snapshots registrados:")
				for i, s := range idx.Snapshots {
					// Verificar si el archivo .tar.gz existe
					archivePath := filepath.Join(snapsDir, s.ID+".tar.gz")
					exists := fileExists(archivePath)
					status := "✅"
					if !exists {
						status = "❌"
					}
					
					fmt.Printf("   [%d] %s - %s %s\n", i+1, s.ID, s.Message, status)
				}
			}
		}
	}
	
	if fileExists(snapsDir) {
		fmt.Println("\n🗂️  Archivos en snapshots/:")
		entries, _ := os.ReadDir(snapsDir)
		tarFiles := []string{}
		otherFiles := []string{}
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".tar.gz") {
				tarFiles = append(tarFiles, entry.Name())
			} else {
				otherFiles = append(otherFiles, entry.Name())
			}
		}
		
		if len(tarFiles) == 0 {
			fmt.Println("   📭 (sin archivos .tar.gz)")
		} else {
			fmt.Printf("   📦 Archivos .tar.gz (%d):\n", len(tarFiles))
			for i, file := range tarFiles {
				if i < 10 { // Mostrar solo primeros 10
					fmt.Printf("   • %s\n", file)
				}
			}
			if len(tarFiles) > 10 {
				fmt.Printf("   ... y %d más\n", len(tarFiles)-10)
			}
		}
		
		if len(otherFiles) > 0 {
			fmt.Printf("\n   📄 Otros archivos (%d):\n", len(otherFiles))
			for _, file := range otherFiles {
				fmt.Printf("   • %s\n", file)
			}
		}
	}
	
	// Mostrar información de configuración si existe
	if fileExists(configPath) {
		fmt.Println("\n⚙️  Configuración cargada:")
		config, err := loadConfig(root)
		if err != nil {
			fmt.Printf("   ❌ Error cargando configuración: %v\n", err)
		} else {
			fmt.Printf("   🎯 Máximo de snapshots: %d\n", config.MaxSnapshots)
			fmt.Printf("   🗑️  Papelera habilitada: %v\n", config.EnableTrash)
			fmt.Printf("   🚫 Ignorar automático: %v\n", strings.Join(config.AutoIgnore, ", "))
		}
	}
	
	fmt.Println("\n💡 Comandos útiles:")
	fmt.Println("   snapgo list        : Listar snapshots")
	fmt.Println("   snapgo status      : Ver estado actual")
	fmt.Println("   snapgo init        : Crear repositorio nuevo")
	
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func formatTime(timestamp string) string {
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return timestamp
	}
	return t.Format("02/01 15:04")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}