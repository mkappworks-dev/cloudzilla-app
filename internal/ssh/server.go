package ssh

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gliderlabs/ssh"
	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/pktline"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp/capability"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	gossh "golang.org/x/crypto/ssh"
)

// sshIdleTimeout closes a connection idle this long. It resets on any
// transfer, so it bounds slow-loris connections without cutting an active push.
const sshIdleTimeout = 60 * time.Second

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
		IdleTimeout:      sshIdleTimeout,
		MaxTimeout:       cfg.SSHMaxSession,
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

	// 1. Try user SSH key (existing behaviour)
	user, err := s.services.SSHKey.AuthenticatePublicKey(ctx, gosshKey)
	if err == nil {
		ctx.SetValue("cloudzilla_user", user)
		return true
	}
	// The same key may also be registered as a deploy key, which mustn't let a suspended owner back in.
	if errors.Is(err, service.ErrAccountSuspended) {
		return false
	}

	// 2. Try deploy key
	dk, err := s.services.DeployKey.AuthenticatePublicKey(ctx, gosshKey)
	if err == nil {
		keyID := dk.ID
		concurrency.Go("deploy_key.update_last_used", func() {
			bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.services.DeployKey.UpdateLastUsed(bg, keyID); err != nil {
				slog.Warn("deploy key last_used update failed", "key_id", keyID, "error", err)
			}
		})
		ctx.SetValue("cloudzilla_deploy_key", dk)
		return true
	}

	return false
}

func (s *Server) sessionHandler(session ssh.Session) {
	cmd := session.Command()
	if len(cmd) == 0 {
		exitWithError(session, "no git command provided\n")
		return
	}

	gitCmd := cmd[0]
	if gitCmd != "git-upload-pack" && gitCmd != "git-receive-pack" {
		exitWithError(session, "unsupported git command: %s\n", gitCmd)
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
		exitWithError(session, "git command requires repository path\n")
		return
	}

	userVal := session.Context().Value("cloudzilla_user")
	dkVal := session.Context().Value("cloudzilla_deploy_key")

	if userVal == nil && dkVal == nil {
		exitWithError(session, "authentication required\n")
		return
	}

	pathParts := strings.Trim(repoPath, "/")
	pathParts = strings.TrimSuffix(pathParts, ".git")
	parts := strings.Split(pathParts, "/")
	if len(parts) != 2 {
		exitWithError(session, "invalid repository path format\n")
		return
	}

	owner, repoName := parts[0], parts[1]

	ctx := session.Context()
	repo, err := s.services.Repo.Get(ctx, owner, repoName)
	if err != nil || !s.canSee(ctx, repo, userVal, dkVal) {
		exitWithError(session, "repository not found\n")
		return
	}

	var pusherName string
	var pusherID int64

	if dkVal != nil {
		dk := dkVal.(*model.DeployKey)

		// Enforce repo binding — deploy key is scoped to one repo
		if repo.ID != dk.RepoID {
			exitWithError(session, "deploy key not authorized for this repository\n")
			return
		}

		// deploy_keys records no creator, so a personal repo's owner is the only
		// person a key can be traced to; a suspended owner could otherwise keep pushing.
		if repo.OwnerID != 0 {
			if owner, err := s.services.User.GetByID(ctx, repo.OwnerID); err != nil || owner.Suspended() {
				exitWithError(session, "repository owner's account is suspended\n")
				return
			}
		}

		// Enforce read-only restriction
		if gitCmd == "git-receive-pack" && dk.ReadOnly {
			exitWithError(session, "deploy key is read-only\n")
			return
		}
		// pusherName stays "" for deploy key pushes
	} else {
		user := userVal.(*model.User)
		pusherName = user.Username
		pusherID = user.ID

		if gitCmd == "git-receive-pack" && !s.services.Repo.CanWrite(ctx, repo, user.ID) {
			exitWithError(session, "access denied\n")
			return
		}
	}

	if gitCmd == "git-receive-pack" {
		if err := service.CheckContentWritable(repo); err != nil {
			exitWithError(session, "%s\n", service.PushRefusal(err))
			return
		}
	}

	diskRepoPath, err := service.RepoDir(s.cfg.ReposRoot, owner, repoName+".git")
	if err != nil {
		exitWithError(session, "repository not found\n")
		return
	}
	gitRepo, err := gogit.PlainOpen(diskRepoPath)
	if err != nil {
		exitWithError(session, "failed to open repository\n")
		return
	}

	vet := func(cmd *packp.Command) error {
		return s.services.BranchProtection.CheckPushCommand(ctx, repo.ID, gitRepo, cmd)
	}
	commands, err := s.execGitService(session, gitCmd, gitRepo, vet, owner, repoName, pusherName)
	if err != nil {
		exitWithError(session, "error: %v\n", err)
		return
	}

	// Dispatch push webhooks for each updated branch.
	if gitCmd == "git-receive-pack" {
		for _, cmd := range commands {
			if !strings.HasPrefix(cmd.Name.String(), "refs/heads/") {
				continue
			}
			if cmd.Action() == packp.Delete {
				continue
			}
			branch := strings.TrimPrefix(cmd.Name.String(), "refs/heads/")
			payload := s.services.Webhook.PushPayload(*repo, pusherName, branch, cmd.New.String())
			repoID := repo.ID
			concurrency.Go("webhook.dispatch.push", func() {
				s.services.Webhook.Dispatch(repoID, "push", payload)
			})
		}

		// Record push activity-feed events (one per updated branch).
		// Deploy-key pushes have no human actor, so they are skipped.
		if dkVal == nil {
			repoID := repo.ID
			repoName, ownerName := repo.Name, repo.OwnerName
			concurrency.Go("event.record.push", func() {
				for _, ps := range s.services.Repo.PushSummaries(gitRepo, commands) {
					s.services.Event.RecordPush(context.Background(), pusherID, pusherName, &repoID, repoName, ownerName, ps)
				}
			})
		}

		if dkVal == nil {
			actor := service.CloseActor{UserID: pusherID, Username: pusherName}
			concurrency.Go("issue_closer.close_for_push", func() {
				bg, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				s.services.IssueCloser.CloseForPush(bg, actor, repo, gitRepo, commands)
			})
		}

		concurrency.Go("repo.on_post_receive", func() {
			bg, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := s.services.Repo.OnPostReceive(bg, repo, gitRepo, commands); err != nil {
				slog.Error("post-receive: commit stats ingest failed",
					"repo_id", repo.ID, "owner", repo.OwnerName, "repo", repo.Name, "error", err)
			}
		})
	}

	_ = session.Exit(0)
}

// canSee reports whether the caller may learn that repo exists: anyone who can
// read it, and a deploy key for it. Any other caller is told it doesn't exist, as
// for a missing repo, so a private repo's existence isn't confirmed.
func (s *Server) canSee(ctx context.Context, repo *model.Repository, userVal, dkVal any) bool {
	if dkVal != nil {
		return repo.ID == dkVal.(*model.DeployKey).RepoID || s.services.Repo.CanRead(ctx, repo, nil)
	}
	return s.services.Repo.CanRead(ctx, repo, &userVal.(*model.User).ID)
}

// exitWithError reports a failure on stderr, which git prints as-is, and ends the
// session with status 1. Stdout carries the pack protocol, where git would read
// the message's first four bytes as a pkt-line length.
// Write and Exit fail only once the client has gone, so their errors are dropped.
func exitWithError(session ssh.Session, format string, args ...any) {
	_, _ = fmt.Fprintf(session.Stderr(), format, args...)
	_ = session.Exit(1)
}

// isFlushOnly reports whether the client's request is a lone flush-pkt, which git
// sends when it needs nothing (ls-remote, an up-to-date fetch or push) and go-git
// rejects as malformed.
func isFlushOnly(r *bufio.Reader) bool {
	p, err := r.Peek(len(pktline.FlushPkt))
	return err == nil && bytes.Equal(p, pktline.FlushPkt)
}

// execGitService runs the git pack protocol over the SSH session and returns
// the commands go-git applied (non-nil only for git-receive-pack).
func (s *Server) execGitService(session ssh.Session, svc string, gitRepo *gogit.Repository, vet func(*packp.Command) error, ownerName, repoName, pusherName string) ([]*packp.Command, error) {
	ep, err := transport.NewEndpoint("/")
	if err != nil {
		return nil, fmt.Errorf("create endpoint: %w", err)
	}

	srv := gittransport.NewServer(gitRepo.Storer, vet)

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

		in := bufio.NewReader(session)
		if isFlushOnly(in) {
			return nil, nil
		}
		req := packp.NewUploadPackRequest()
		if err := req.Decode(in); err != nil {
			return nil, fmt.Errorf("decode upload-pack request: %w", err)
		}

		resp, err := sess.UploadPack(session.Context(), req)
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

		// Nothing reading the request may close the session: go-git closes
		// the packfile reader after ingestion, and sessionHandler still
		// writes the status and the exit code.
		limiter := gittransport.NewLimitedReadCloser(io.NopCloser(session), s.cfg.MaxPackBytes)
		counter := gittransport.NewByteCounter(limiter)
		in := bufio.NewReader(counter)
		if isFlushOnly(in) {
			return nil, nil
		}

		req := packp.NewReferenceUpdateRequest()
		if err := req.Decode(in); err != nil {
			return nil, fmt.Errorf("decode receive-pack request: %w", err)
		}

		start := time.Now()
		status, err := sess.ReceivePack(session.Context(), req)
		if err != nil {
			if limiter.Exceeded() {
				return nil, fmt.Errorf("pack exceeds maximum allowed size (%d bytes)", s.cfg.MaxPackBytes)
			}
			return nil, fmt.Errorf("receive-pack: %w", err)
		}
		refsOK, refsFailed := gittransport.CountRefStatus(status)
		slog.Info("ssh: receive-pack complete",
			"owner", ownerName,
			"repo", repoName,
			"pusher", pusherName,
			"commands", len(req.Commands),
			"refs_ok", refsOK,
			"refs_failed", refsFailed,
			"pack_bytes", counter.Bytes(),
			"duration_ms", time.Since(start).Milliseconds(),
		)

		if status != nil && req.Capabilities.Supports(capability.ReportStatus) {
			if err := status.Encode(session); err != nil {
				return nil, fmt.Errorf("encode receive-pack status: %w", err)
			}
		}

		return gittransport.AppliedCommands(status, req.Commands), nil
	}
}
