package ssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"

	gossh "golang.org/x/crypto/ssh"
	"github.com/gliderlabs/ssh"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/mkappworks/cloudzilla/internal/config"
	"github.com/mkappworks/cloudzilla/internal/model"
	"github.com/mkappworks/cloudzilla/internal/service"
)

type Server struct {
	cfg      config.GitConfig
	services *service.Services
	srv      *ssh.Server
}

func New(cfg config.GitConfig, services *service.Services) *Server {
	s := &Server{
		cfg:      cfg,
		services: services,
	}

	hostKey, err := s.loadOrGenerateHostKey()
	if err != nil {
		panic(fmt.Sprintf("failed to load/generate host key: %v", err))
	}

	s.srv = &ssh.Server{
		Addr:             fmt.Sprintf(":%d", cfg.SSHPort),
		Handler:          s.sessionHandler,
		PublicKeyHandler: s.publicKeyHandler,
		HostSigners:      []ssh.Signer{hostKey},
	}

	return s
}

func (s *Server) ListenAndServe() error {
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Close()
}

func (s *Server) loadOrGenerateHostKey() (ssh.Signer, error) {
	keyPath := s.cfg.SSHHostKey

	// Try to load existing key
	keyData, err := ioutil.ReadFile(keyPath)
	if err == nil {
		// Parse the loaded key
		signer, err := gossh.ParsePrivateKey(keyData)
		if err == nil {
			return signer, nil
		}
	}

	// Generate new key
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	// Encode as PEM
	privKeyBlock, err := gossh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}

	// Encode PEM block to bytes
	privKeyBytes := pem.EncodeToMemory(privKeyBlock)

	// Write to file
	if err := ioutil.WriteFile(keyPath, privKeyBytes, 0600); err != nil {
		return nil, fmt.Errorf("write key file: %w", err)
	}

	// Create signer
	signer, err := gossh.NewSignerFromKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("create signer: %w", err)
	}

	return signer, nil
}

func (s *Server) publicKeyHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	// Convert gliderlabs/ssh.PublicKey to golang.org/x/crypto/ssh.PublicKey
	gosshKey, err := gossh.ParsePublicKey(key.Marshal())
	if err != nil {
		return false
	}

	// Authenticate user by public key
	user, err := s.services.SSHKey.AuthenticatePublicKey(ctx, gosshKey)
	if err != nil {
		return false
	}

	// Store user in context for later use in session handler
	ctx.SetValue("cloudzilla_user", user)
	return true
}

func (s *Server) sessionHandler(session ssh.Session) {
	cmd := session.Command()
	if len(cmd) == 0 {
		fmt.Fprintf(session, "no git command provided\n")
		session.Exit(1)
		return
	}

	gitCmd := cmd[0]
	if gitCmd != "git-upload-pack" && gitCmd != "git-receive-pack" {
		fmt.Fprintf(session, "unsupported git command: %s\n", gitCmd)
		session.Exit(1)
		return
	}

	// Extract repository path from argument
	var repoPath string
	if len(cmd) > 1 {
		repoPath = cmd[1]
		// Strip surrounding quotes if present
		repoPath = strings.Trim(repoPath, "'\"")
		// Ensure it ends with .git
		if !strings.HasSuffix(repoPath, ".git") {
			repoPath = repoPath + ".git"
		}
	} else {
		fmt.Fprintf(session, "git command requires repository path\n")
		session.Exit(1)
		return
	}

	// Get user from context
	userVal := session.Context().Value("cloudzilla_user")
	if userVal == nil {
		fmt.Fprintf(session, "authentication required\n")
		session.Exit(1)
		return
	}
	user := userVal.(*model.User)

	// Parse repo path: /owner/repo.git or owner/repo.git
	pathParts := strings.Trim(repoPath, "/")
	pathParts = strings.TrimSuffix(pathParts, ".git")
	parts := strings.Split(pathParts, "/")
	if len(parts) != 2 {
		fmt.Fprintf(session, "invalid repository path format\n")
		session.Exit(1)
		return
	}

	owner, repoName := parts[0], parts[1]

	// Get repository from service
	ctx := session.Context()
	repo, err := s.services.Repo.Get(ctx, owner, repoName)
	if err != nil {
		fmt.Fprintf(session, "repository not found\n")
		session.Exit(1)
		return
	}

	// Check permissions
	if gitCmd == "git-upload-pack" {
		if !s.services.Repo.CanRead(ctx, repo, &user.ID) {
			fmt.Fprintf(session, "access denied\n")
			session.Exit(1)
			return
		}
	} else if gitCmd == "git-receive-pack" {
		if !s.services.Repo.CanWrite(ctx, repo, user.ID) {
			fmt.Fprintf(session, "access denied\n")
			session.Exit(1)
			return
		}
	}

	// Open git repository
	diskRepoPath := filepath.Join(s.cfg.ReposRoot, owner, repoName+".git")
	gitRepo, err := gogit.PlainOpen(diskRepoPath)
	if err != nil {
		fmt.Fprintf(session, "failed to open repository\n")
		session.Exit(1)
		return
	}

	// Execute git service
	if err := s.execGitService(session, gitCmd, gitRepo); err != nil {
		fmt.Fprintf(session, "error: %v\n", err)
		session.Exit(1)
		return
	}

	session.Exit(0)
}

func (s *Server) execGitService(session ssh.Session, service string, gitRepo *gogit.Repository) error {
	storer := gitRepo.Storer
	ep, err := transport.NewEndpoint("/")
	if err != nil {
		return fmt.Errorf("create endpoint: %w", err)
	}

	srv := server.NewServer(server.MapLoader{"/": storer})

	if service == "git-upload-pack" {
		sess, err := srv.NewUploadPackSession(ep, nil)
		if err != nil {
			return fmt.Errorf("create upload pack session: %w", err)
		}

		// Send advertised references
		ar, err := sess.AdvertisedReferences()
		if err != nil {
			return fmt.Errorf("get advertised references: %w", err)
		}

		// Write advertised refs to session
		var buf bytes.Buffer
		if err := ar.Encode(&buf); err != nil {
			return fmt.Errorf("encode advertised refs: %w", err)
		}

		// Write to stdout and read from stdin for pack protocol
		// This is a simplified implementation
		io.Copy(session, &buf)

	} else if service == "git-receive-pack" {
		sess, err := srv.NewReceivePackSession(ep, nil)
		if err != nil {
			return fmt.Errorf("create receive pack session: %w", err)
		}

		// Send advertised references
		ar, err := sess.AdvertisedReferences()
		if err != nil {
			return fmt.Errorf("get advertised references: %w", err)
		}

		// Write advertised refs to session
		var buf bytes.Buffer
		if err := ar.Encode(&buf); err != nil {
			return fmt.Errorf("encode advertised refs: %w", err)
		}

		// Write to stdout and read from stdin for pack protocol
		// This is a simplified implementation
		io.Copy(session, &buf)

		_ = sess // TODO: process pack data from stdin
	}

	return nil
}
