package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

// securityRepo answers the missing-repo 404 to anyone who cannot write, so the tab does not reveal itself.
func (h *Handler) securityRepo(w http.ResponseWriter, r *http.Request) (*model.Repository, int64, bool) {
	repo, err := h.Services.Repo.Get(r.Context(), chi.URLParam(r, "owner"), chi.URLParam(r, "repo"))
	viewer := viewerOf(r)
	if err != nil || viewer == nil || !h.Services.Repo.CanWrite(r.Context(), repo, *viewer) {
		h.NotFound(w, r)
		return nil, 0, false
	}
	return repo, *viewer, true
}

// securityAlert loads the alert named by the URL, answering 404 for a malformed or unknown number.
func (h *Handler) securityAlert(w http.ResponseWriter, r *http.Request, repo *model.Repository) (*model.SecretScanAlert, bool) {
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		h.NotFound(w, r)
		return nil, false
	}
	alert, err := h.Services.SecretAlert.Get(r.Context(), repo.ID, number)
	if errors.Is(err, service.ErrSecretAlertNotFound) {
		h.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		slog.Error("security: load alert failed", "repo_id", repo.ID, "number", number, "error", err)
		h.ServerError(w, r)
		return nil, false
	}
	return alert, true
}

// PageSecurityAlerts lists a repository's secret scanning alerts.
func (h *Handler) PageSecurityAlerts(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := h.securityRepo(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	state := model.SecretAlertOpen
	if r.URL.Query().Get("state") == model.SecretAlertClosed {
		state = model.SecretAlertClosed
	}
	typeID := r.URL.Query().Get("type")
	if view.SecretTypeLabel(typeID) == typeID {
		typeID = ""
	}
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}

	alerts, total, err := h.Services.SecretAlert.List(ctx, repo.ID, state, typeID, page)
	if err != nil {
		slog.Error("security: list alerts failed", "repo_id", repo.ID, "error", err)
		h.ServerError(w, r)
		return
	}
	otherState := model.SecretAlertClosed
	if state == model.SecretAlertClosed {
		otherState = model.SecretAlertOpen
	}
	_, otherTotal, err := h.Services.SecretAlert.List(ctx, repo.ID, otherState, typeID, 1)
	if err != nil {
		slog.Error("security: count alerts failed", "repo_id", repo.ID, "error", err)
		h.ServerError(w, r)
		return
	}

	base := "/" + repo.OwnerName + "/" + repo.Name + "/security/secrets"
	data := view.SecretAlertsData{
		BasePage: h.withRepoSubnav(ctx, basePage(r, h.Services), repo, "security", h.canManageRepo(r, repo)),
		Owner:    repo.OwnerName, RepoName: repo.Name,
		State: state, Type: typeID, PushProtection: h.Cfg.SecretScanning.PushProtection,
		OpenTabURL:   securityListURL(base, model.SecretAlertOpen, typeID, 1),
		ClosedTabURL: securityListURL(base, model.SecretAlertClosed, typeID, 1),
	}
	if state == model.SecretAlertOpen {
		data.OpenCount, data.ClosedCount = total, otherTotal
	} else {
		data.OpenCount, data.ClosedCount = otherTotal, total
	}
	for _, t := range view.SecretTypes {
		data.TypeOptions = append(data.TypeOptions, view.SecretTypeOption{Value: t.ID, Label: t.Label, Selected: t.ID == typeID})
	}

	scanState, scanIncomplete, err := h.Services.SecretScan.ScanState(ctx, repo.ID)
	if err != nil {
		slog.Error("security: read history scan state failed", "repo_id", repo.ID, "error", err)
		h.ServerError(w, r)
		return
	}
	data.HistoryScanning = scanState != "done"
	data.HistoryIncomplete = scanState == "done" && scanIncomplete

	names := newUsernameCache(h.Services.User)
	for _, a := range alerts {
		data.Rows = append(data.Rows, h.securityRow(ctx, repo, a, names))
	}
	if page > 1 {
		data.NewerURL = securityListURL(base, state, typeID, page-1)
	}
	if page*25 < total {
		data.OlderURL = securityListURL(base, state, typeID, page+1)
	}
	h.render(w, r, pages.SecurityAlerts(data))
}

func securityListURL(base, state, typeID string, page int) string {
	q := []string{"state=" + state}
	if typeID != "" {
		q = append(q, "type="+typeID)
	}
	if page > 1 {
		q = append(q, "page="+strconv.Itoa(page))
	}
	return base + "?" + strings.Join(q, "&")
}

func (h *Handler) canManageRepo(r *http.Request, repo *model.Repository) bool {
	viewer := viewerOf(r)
	return viewer != nil && h.Services.Repo.CanManage(r.Context(), repo, *viewer)
}

func (h *Handler) securityRow(ctx context.Context, repo *model.Repository, a model.SecretScanAlert, names *usernameCache) view.SecretAlertRow {
	row := view.SecretAlertRow{
		Number:      a.Number,
		URL:         "/" + repo.OwnerName + "/" + repo.Name + "/security/secrets/" + strconv.Itoa(a.Number),
		TypeLabel:   view.SecretTypeLabel(a.SecretType),
		Hint:        view.SecretHintDisplay(a.SecretType, a.SecretHint),
		Closed:      a.State == model.SecretAlertClosed,
		Blocked:     a.Blocked && a.State == model.SecretAlertOpen,
		AutoRevoked: a.AutoRevoked,
		Incomplete:  a.Incomplete,
		DetectedAt:  a.CreatedAt,
	}
	if row.Closed {
		row.ResolutionLabel = view.SecretResolutionLabel(a.Resolution)
		if a.ResolvedAt != nil {
			row.ClosedAt = *a.ResolvedAt
		}
		if a.ResolvedBy != nil {
			row.ClosedBy = names.get(ctx, *a.ResolvedBy)
		}
	}
	locs, err := h.Services.SecretAlert.Locations(ctx, a.ID)
	if err != nil {
		slog.Warn("security: load locations failed", "alert_id", a.ID, "error", err)
	}
	if loc := primaryLocation(locs); loc != nil {
		row.Location = loc.FilePath + ":" + strconv.Itoa(loc.LineNumber)
		if hasCommit(loc.CommitSHA) {
			row.CommitShort = view.ShortSHA(loc.CommitSHA)
			row.CommitURL = "/" + repo.OwnerName + "/" + repo.Name + "/commit/" + loc.CommitSHA
		}
	}
	if evs, err := h.Services.SecretAlert.Events(ctx, a.ID); err == nil {
		if ev := firstSighting(evs); ev != nil && ev.ActorID != nil {
			row.Pusher = names.get(ctx, *ev.ActorID)
		}
	}
	return row
}

// hasCommit is false for the all-zero SHA of a web edit that was refused: it names no commit.
func hasCommit(sha string) bool { return sha != "" && sha != plumbing.ZeroHash.String() }

// primaryLocation prefers a location a ref reaches: the commit a reader can still open.
func primaryLocation(locs []model.SecretScanLocation) *model.SecretScanLocation {
	for i := range locs {
		if locs[i].ReachedRef {
			return &locs[i]
		}
	}
	if len(locs) > 0 {
		return &locs[0]
	}
	return nil
}

func firstSighting(evs []model.SecretScanEvent) *model.SecretScanEvent {
	for i := range evs {
		if evs[i].Kind == model.SecretEventFound || evs[i].Kind == model.SecretEventBlocked {
			return &evs[i]
		}
	}
	return nil
}

// PageSecurityAlert shows one alert.
func (h *Handler) PageSecurityAlert(w http.ResponseWriter, r *http.Request) {
	repo, _, ok := h.securityRepo(w, r)
	if !ok {
		return
	}
	alert, ok := h.securityAlert(w, r, repo)
	if !ok {
		return
	}
	ctx := r.Context()
	base := "/" + repo.OwnerName + "/" + repo.Name
	names := newUsernameCache(h.Services.User)

	locs, err := h.Services.SecretAlert.Locations(ctx, alert.ID)
	if err != nil {
		slog.Error("security: load locations failed", "alert_id", alert.ID, "error", err)
		h.ServerError(w, r)
		return
	}
	events, err := h.Services.SecretAlert.Events(ctx, alert.ID)
	if err != nil {
		slog.Error("security: load events failed", "alert_id", alert.ID, "error", err)
		h.ServerError(w, r)
		return
	}

	data := view.SecretAlertData{
		BasePage:      h.withRepoSubnav(ctx, basePage(r, h.Services), repo, "security", h.canManageRepo(r, repo)),
		Owner:         repo.OwnerName,
		RepoName:      repo.Name,
		ListURL:       base + "/security/secrets",
		ActionURL:     base + "/security/secrets/" + strconv.Itoa(alert.Number),
		Number:        alert.Number,
		TypeID:        alert.SecretType,
		TypeLabel:     view.SecretTypeLabel(alert.SecretType),
		Hint:          view.SecretHintDisplay(alert.SecretType, alert.SecretHint),
		Closed:        alert.State == model.SecretAlertClosed,
		Blocked:       alert.Blocked,
		AutoRevoked:   alert.AutoRevoked,
		Incomplete:    alert.Incomplete,
		CanAllow:      alert.SecretType != model.SecretTypeCloudzillaToken,
		DetectedAt:    alert.CreatedAt,
		HowToFix:      view.SecretHowToFix(alert.SecretType),
		DefaultBranch: repo.DefaultBranch,
	}
	if data.Closed {
		data.ResolutionLabel = view.SecretResolutionLabel(alert.Resolution)
	}

	if first := firstSighting(events); first != nil {
		data.Ref = first.Ref
		if first.ActorID != nil {
			data.Pusher = names.get(ctx, *first.ActorID)
		}
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == model.SecretEventBlocked {
			data.BlockedAt = events[i].CreatedAt
			if events[i].ActorID != nil {
				data.BlockedBy = names.get(ctx, *events[i].ActorID)
			}
			break
		}
	}
	for _, ev := range events {
		data.Events = append(data.Events, h.securityEvent(ctx, repo, ev, names))
	}

	for _, l := range locs {
		lv := view.SecretLocationView{Text: l.FilePath + ":" + strconv.Itoa(l.LineNumber)}
		if hasCommit(l.CommitSHA) {
			lv.CommitShort = view.ShortSHA(l.CommitSHA)
			lv.CommitURL = base + "/commit/" + l.CommitSHA
		}
		data.Locations = append(data.Locations, lv)
		if data.OnDefaultBranch || !hasCommit(l.CommitSHA) {
			continue
		}
		if on, err := h.Services.Code.CommitOnBranch(repo.OwnerName, repo.Name, l.CommitSHA, repo.DefaultBranch); err == nil && on {
			data.OnDefaultBranch = true
			data.DefaultCommit = view.ShortSHA(l.CommitSHA)
			data.DefaultCommitURL = base + "/commit/" + l.CommitSHA
		}
	}

	if loc := primaryLocation(locs); loc != nil && !hasCommit(loc.CommitSHA) {
		data.Snippet = view.SecretSnippetView{Path: loc.FilePath, NoCommit: true}
	} else if loc != nil {
		data.CommitShort = view.ShortSHA(loc.CommitSHA)
		data.CommitURL = base + "/commit/" + loc.CommitSHA
		data.Snippet = h.securitySnippet(repo, alert, *loc)
	} else {
		data.Snippet.Gone = true
	}
	h.render(w, r, pages.SecurityAlert(data))
}

func (h *Handler) securitySnippet(repo *model.Repository, alert *model.SecretScanAlert, loc model.SecretScanLocation) view.SecretSnippetView {
	out := view.SecretSnippetView{
		Path:        loc.FilePath,
		CommitShort: view.ShortSHA(loc.CommitSHA),
		FileURL:     "/" + repo.OwnerName + "/" + repo.Name + "/blob/" + loc.CommitSHA + "/" + loc.FilePath + "#L" + strconv.Itoa(loc.LineNumber),
	}
	snip, err := h.Services.Code.SecretSnippet(repo.OwnerName, repo.Name, alert, loc)
	if err != nil {
		if !errors.Is(err, service.ErrCommitGone) {
			slog.Warn("security: read snippet failed", "alert_id", alert.ID, "error", err)
		}
		out.Gone = true
		return out
	}
	for _, l := range snip.Lines {
		out.Lines = append(out.Lines, view.SecretSnippetViewLine{Number: l.Number, Secret: l.Secret, Before: l.Before, Mask: l.Mask, After: l.After})
	}
	return out
}

func (h *Handler) securityEvent(ctx context.Context, repo *model.Repository, ev model.SecretScanEvent, names *usernameCache) view.SecretEventView {
	out := view.SecretEventView{Kind: ev.Kind, Ref: ev.Ref, Detail: ev.Detail, When: ev.CreatedAt}
	if hasCommit(ev.CommitSHA) {
		out.CommitShort = view.ShortSHA(ev.CommitSHA)
		out.CommitURL = "/" + repo.OwnerName + "/" + repo.Name + "/commit/" + ev.CommitSHA
	}
	if ev.ActorID != nil {
		out.Actor = names.get(ctx, *ev.ActorID)
		out.ActorURL = "/" + out.Actor
	}
	if ev.Kind == model.SecretEventClosed || ev.Kind == model.SecretEventAllowed {
		out.Detail = strings.ToLower(view.SecretResolutionLabel(ev.Detail))
	}
	return out
}

// usernameCache resolves each actor once per request; a deleted user reads as "a deleted user".
type usernameCache struct {
	users *service.UserService
	seen  map[int64]string
}

func newUsernameCache(users *service.UserService) *usernameCache {
	return &usernameCache{users: users, seen: map[int64]string{}}
}

func (c *usernameCache) get(ctx context.Context, id int64) string {
	if name, ok := c.seen[id]; ok {
		return name
	}
	name := "a deleted user"
	if u, err := c.users.GetByID(ctx, id); err == nil {
		name = u.Username
	}
	c.seen[id] = name
	return name
}
