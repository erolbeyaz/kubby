package kubectlsh

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Upload puts a file into the session's working directory, so the manifest or chart on
// the reader's own machine is the one the command they type acts on.
//
// This is what makes the terminal answer the question people actually bring to it —
// `kubectl apply -f ingress.yaml` — rather than only reading what is already in the
// cluster. The files live as long as the session and go with it.
func (s *Session) Upload(name string, data []byte) error {
	clean, err := safeRelativePath(name)
	if err != nil {
		return err
	}

	if int64(len(data)) > maxUploadBytes {
		return fmt.Errorf("%s is larger than the %d MB a session accepts", clean, maxUploadBytes>>20)
	}

	s.mu.Lock()
	total := s.uploaded + int64(len(data))
	if total > maxSessionBytes {
		s.mu.Unlock()
		return fmt.Errorf("this session has reached its %d MB of uploaded files", maxSessionBytes>>20)
	}
	s.uploaded = total
	s.mu.Unlock()

	target := filepath.Join(s.fileDir, clean)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("could not create %s: %w", filepath.Dir(clean), err)
	}
	// 0600: a manifest can carry a secret, and nothing else on this machine has business
	// reading what one reader dropped into their own session.
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("could not write %s: %w", clean, err)
	}

	s.write(fmt.Sprintf("\x1b[2m↱ \x1b[0m%s\x1b[2m  %s\x1b[0m\r\n", clean, humanSize(len(data))))
	return nil
}

// safeRelativePath keeps an upload inside the session's own directory.
//
// A name is data from the browser, so it is treated as hostile: an absolute path, a
// `..`, or a Windows drive letter would each put a file somewhere this session has no
// business writing — the kubeconfig beside it being the obvious target.
func safeRelativePath(name string) (string, error) {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if name == "" {
		return "", fmt.Errorf("a file needs a name")
	}
	if strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("that file name is not usable")
	}
	// A drive letter is absolute on Windows but looks relative to filepath here.
	if len(name) > 1 && name[1] == ':' {
		return "", fmt.Errorf("%q is an absolute path", name)
	}
	if strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("%q is an absolute path", name)
	}

	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == string(filepath.Separator) {
		return "", fmt.Errorf("a file needs a name")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q reaches outside this session", name)
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("%q is an absolute path", name)
	}
	return clean, nil
}

func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

const (
	// A manifest or a chart, not a container image. Anything larger is a sign the wrong
	// thing was dragged in.
	maxUploadBytes  = 8 << 20
	maxSessionBytes = 64 << 20
)
