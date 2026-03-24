package ssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gossh "golang.org/x/crypto/ssh"
	"github.com/gliderlabs/ssh"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
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
	keyData, err := os.ReadFile(keyPath)
	if err == nil {
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

	privKeyBlock, err := gossh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}

	privKeyBytes := pem.EncodeToMemory(privKeyBlock)

	if err := os.WriteFile(keyPath, privKeyBytes, 0600); err != nil {
		return nil, fmt.Errorf("write key file: %w", err)
	}

	signer, err := gossh.NewSignerFromKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("create signer: %w", err)
	}

	return signer, nil
}

func (s *Server) publicKeyHandler(ctx ssh.Context, key ssh.PublicKey) bool {
	gosshKey, err := gossh.ParsePublicKey(key.Marshal())
	if err != nil {
		return false
	}

	user, err := s.services.SSHKey.AuthenticatePublicKey(ctx, gosshKey)
	if err != nil {
		return false
	}

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

	var repoPath string
	if len(cmd) > 1 {
		repoPath = cmd[1]
		repoPath = strings.Trim(repoPath, "'\"")
		if !strings.HasSuffix(repoPath, ".git") {
			repoPath = repoPath + ".git"
		}
	} else {
		fmt.Fprintf(session, "git command requires repository path\n")
		session.Exit(1)
		return
	}

	userVal := session.Context().Value("cloudzilla_user")
	if userVal == nil {
		fmt.Fprintf(session, "authentication required\n")
		session.Exit(1)
		return
	}
	user := userVal.(*model.User)

	pathParts := strings.Trim(repoPath, "/")
	pathParts = strings.TrimSuffix(pathParts, ".git")
	parts := strings.Split(pathParts, "/")
	if len(parts) != 2 {
		fmt.Fprintf(session, "invalid repository path format\n")
		session.Exit(1)
		return
	}

	owner, repoName := parts[0], parts[1]

	ctx := session.Context()
	repo, err := s.services.Repo.Get(ctx, owner, repoName)
	if err != nil {
		fmt.Fprintf(session, "repository not found\n")
		session.Exit(1)
		return
	}

	if gitCmd == "git-upload-pack" {
		if !s.services.Repo.CanRead(ctx, repo, &user.ID) {
			fmt.Fprintf(session, "access denied\n")
			session.Exit(1)
			return
		}
	} else {
		if !s.services.Repo.CanWrite(ctx, repo, user.ID) {
			fmt.Fprintf(session, "access denied\n")
			session.Exit(1)
			return
		}
	}

	diskRepoPath := filepath.Join(s.cfg.ReposRoot, owner, repoName+".git")
	gitRepo, err := gogit.PlainOpen(diskRepoPath)
	if err != nil {
		fmt.Fprintf(session, "failed to open repository\n")
		session.Exit(1)
		return
	}

	commands, err := s.execGitService(session, gitCmd, gitRepo)
	if err != nil {
		fmt.Fprintf(session, "error: %v\n", err)
		session.Exit(1)
		return
	}

	// Dispatch push webhooks for each updated branch
	if gitCmd == "git-receive-pack" && err == nil {
		for _, cmd := range commands {
			if !strings.HasPrefix(cmd.Name.String(), "refs/heads/") {
				continue
			}
			if cmd.Action() == packp.Delete {
				continue
			}
			branch := strings.TrimPrefix(cmd.Name.String(), "refs/heads/")
			payload := s.services.Webhook.PushPayload(*repo, user.Username, branch, cmd.New.String())
			go s.services.Webhook.Dispatch(repo.ID, "push", payload)
		}
	}

	session.Exit(0)
}

// execGitService runs the git pack protocol over the SSH session and returns
// the pushed commands (non-nil only for git-receive-pack).
func (s *Server) execGitService(session ssh.Session, svc string, gitRepo *gogit.Repository) ([]*packp.Command, error) {
	ep, err := transport.NewEndpoint("/")
	if err != nil {
		return nil, fmt.Errorf("create endpoint: %w", err)
	}

	srv := server.NewServer(server.MapLoader{"/": gitRepo.Storer})

	if svc == "git-upload-pack" {
		sess, err := srv.NewUploadPackSession(ep, nil)
		if err != nil {
			return nil, fmt.Errorf("create upload pack session: %w", err)
		}

		ar, err := sess.AdvertisedReferences()
		if err != nil {
			return nil, fmt.Errorf("get advertised references: %w", err)
		}

		var buf bytes.Buffer
		if err := ar.Encode(&buf); err != nil {
			return nil, fmt.Errorf("encode advertised refs: %w", err)
		}
		if _, err := buf.WriteTo(session); err != nil {
			return nil, fmt.Errorf("write advertised refs: %w", err)
		}

		req := packp.NewUploadPackRequest()
		if err := req.Decode(session); err != nil {
			return nil, fmt.Errorf("decode upload-pack request: %w", err)
		}

		resp, err := sess.UploadPack(context.Background(), req)
		if err != nil {
			return nil, fmt.Errorf("upload-pack: %w", err)
		}

		if err := resp.Encode(session); err != nil {
			return nil, fmt.Errorf("encode upload-pack response: %w", err)
		}

		return nil, nil

	} else { // git-receive-pack
		sess, err := srv.NewReceivePackSession(ep, nil)
		if err != nil {
			return nil, fmt.Errorf("create receive pack session: %w", err)
		}

		ar, err := sess.AdvertisedReferences()
		if err != nil {
			return nil, fmt.Errorf("get advertised references: %w", err)
		}

		var buf bytes.Buffer
		if err := ar.Encode(&buf); err != nil {
			return nil, fmt.Errorf("encode advertised refs: %w", err)
		}
		if _, err := buf.WriteTo(session); err != nil {
			return nil, fmt.Errorf("write advertised refs: %w", err)
		}

		req := packp.NewReferenceUpdateRequest()
		if err := req.Decode(session); err != nil {
			return nil, fmt.Errorf("decode receive-pack request: %w", err)
		}

		status, err := sess.ReceivePack(context.Background(), req)
		if err != nil {
			return nil, fmt.Errorf("receive-pack: %w", err)
		}

		if status != nil {
			if err := status.Encode(session); err != nil {
				return nil, fmt.Errorf("encode receive-pack status: %w", err)
			}
		}

		return req.Commands, nil
	}
}
