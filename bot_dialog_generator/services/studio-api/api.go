package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/auth"
	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/httpserver"
)

// ProblemDetails represents an RFC 9457 Problem Details object.
type ProblemDetails struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
}

func writeProblem(w http.ResponseWriter, status int, title, detail, instance string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ProblemDetails{
		Type:     "https://bot-dialog-generator.example.com/errors/studio-api",
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: instance,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// notFound is returned for missing resources and for resources in workspaces the
// caller cannot see, so existence is never revealed.
func notFound(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, http.StatusNotFound, "Not Found", "The resource does not exist or you do not have access to it.", r.URL.Path)
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid JSON", err.Error(), r.URL.Path)
		return false
	}
	return true
}

func writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		notFound(w, r)
	case errors.Is(err, ErrImmutable):
		writeProblem(w, http.StatusBadRequest, "Immutable Version", "Published versions cannot be changed. Create a new draft instead.", r.URL.Path)
	case errors.Is(err, ErrInvalid):
		// The detail is shown to people as is, so drop the generic "invalid request: " prefix.
		detail := strings.TrimPrefix(err.Error(), ErrInvalid.Error()+": ")
		writeProblem(w, http.StatusBadRequest, "Invalid Request", detail, r.URL.Path)
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Error", "Unexpected error.", r.URL.Path)
	}
}

// Config wires authentication and cross-origin policy into the Studio handler.
type Config struct {
	Verifier auth.Verifier
	// DevIssuer is set only in local development; it enables POST /dev/token.
	DevIssuer          *auth.DevIssuer
	PlatformAdminGroup string
	AllowedOrigins     []string
}

// Principal is the authenticated caller and what it may access.
type Principal struct {
	Subject         string
	IsPlatformAdmin bool
	Roles           map[string]Role // workspaceID -> role
}

// scope is the authorized context of a workspace-scoped request.
type scope struct {
	principal   *Principal
	workspaceID string
	role        Role
}

type studio struct {
	store      Store
	adminGroup string
}

type workspaceRoute struct {
	pattern string
	need    Role
	handle  func(h *studio, w http.ResponseWriter, r *http.Request, s scope)
}

// workspaceRoutes lists every route that reads or changes a workspace's data. The
// isolation tests iterate this table, so a new route is covered automatically.
var workspaceRoutes = []workspaceRoute{
	{"GET /workspaces/{workspaceID}/bots", RoleAnalyst, (*studio).listBots},
	{"POST /workspaces/{workspaceID}/bots", RoleEditor, (*studio).createBot},
	{"GET /workspaces/{workspaceID}/bots/{botID}", RoleAnalyst, (*studio).getBot},
	{"GET /workspaces/{workspaceID}/bots/{botID}/active-version", RoleAnalyst, (*studio).getActiveVersion},
	{"PUT /workspaces/{workspaceID}/bots/{botID}/active-version", RoleOwner, (*studio).setActiveVersion},
	{"GET /workspaces/{workspaceID}/bots/{botID}/versions", RoleAnalyst, (*studio).listVersions},
	{"POST /workspaces/{workspaceID}/bots/{botID}/versions", RoleEditor, (*studio).createDraft},
	{"GET /workspaces/{workspaceID}/bots/{botID}/versions/{versionID}", RoleAnalyst, (*studio).getVersion},
	{"PUT /workspaces/{workspaceID}/bots/{botID}/versions/{versionID}", RoleEditor, (*studio).saveDraft},
	{"POST /workspaces/{workspaceID}/bots/{botID}/versions/{versionID}/publish", RoleOwner, (*studio).publish},
	{"GET /workspaces/{workspaceID}/members", RoleAnalyst, (*studio).listAccess},
	{"PUT /workspaces/{workspaceID}/members/{subject}", RoleOwner, (*studio).setMember},
	{"DELETE /workspaces/{workspaceID}/members/{subject}", RoleOwner, (*studio).removeMember},
	{"PUT /workspaces/{workspaceID}/groups/{groupID}", RoleOwner, (*studio).setGroupGrant},
	{"DELETE /workspaces/{workspaceID}/groups/{groupID}", RoleOwner, (*studio).removeGroupGrant},
}

type adminRoute struct {
	pattern string
	handle  func(h *studio, w http.ResponseWriter, r *http.Request)
}

// adminRoutes are visible only to platform admins, and every call is audited.
var adminRoutes = []adminRoute{
	{"GET /admin/workspaces", (*studio).adminListWorkspaces},
	{"GET /admin/bots", (*studio).adminListBots},
	{"GET /admin/audit", (*studio).adminListAudit},
}

type adminWriteRoute struct {
	pattern string
	handle  func(h *studio, w http.ResponseWriter, r *http.Request, p *Principal)
}

// adminWriteRoutes change workspaces themselves. Only platform admins may call them,
// and each handler audits the change it made.
var adminWriteRoutes = []adminWriteRoute{
	{"POST /admin/workspaces", (*studio).adminCreateWorkspace},
	{"PATCH /admin/workspaces/{workspaceID}", (*studio).adminUpdateWorkspace},
}

// NewStudioHandler returns the HTTP handler with all Studio routes mounted.
func NewStudioHandler(store Store, cfg Config) http.Handler {
	h := &studio{store: store, adminGroup: cfg.PlatformAdminGroup}

	private := http.NewServeMux()
	private.HandleFunc("GET /me", h.me)
	private.HandleFunc("GET /workspaces", h.listMyWorkspaces)
	for _, rt := range workspaceRoutes {
		private.Handle(rt.pattern, h.scoped(rt))
	}
	for _, rt := range adminRoutes {
		private.Handle(rt.pattern, h.admin(rt))
	}
	for _, rt := range adminWriteRoutes {
		private.Handle(rt.pattern, h.adminWrite(rt))
	}

	public := http.NewServeMux()
	health := httpserver.New("studio-api")
	public.Handle("GET /livez", health)
	public.Handle("GET /readyz", health)
	if cfg.DevIssuer != nil {
		public.HandleFunc("POST /dev/token", devToken(cfg.DevIssuer))
	}
	public.Handle("/", auth.Middleware(cfg.Verifier, private))

	return cors(cfg.AllowedOrigins, public)
}

func (h *studio) principal(r *http.Request) *Principal {
	claims, _ := auth.FromContext(r.Context())
	return &Principal{
		Subject:         claims.Subject,
		IsPlatformAdmin: h.adminGroup != "" && slices.Contains(claims.Groups, h.adminGroup),
		Roles:           h.store.RolesFor(claims.Subject, claims.Groups),
	}
}

func (h *studio) audit(p *Principal, workspaceID, action string, r *http.Request) {
	h.store.AppendAudit(AuditEntry{Subject: p.Subject, WorkspaceID: workspaceID, Action: action, Resource: r.URL.Path})
}

// scoped authorizes a workspace route: non-members get 404, members without the
// required role get 403, and platform admins act as owners with every access audited.
func (h *studio) scoped(rt workspaceRoute) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := h.principal(r)
		s := scope{principal: p, workspaceID: r.PathValue("workspaceID")}
		role, member := p.Roles[s.workspaceID]
		switch {
		case member:
			s.role = role
		case p.IsPlatformAdmin && h.store.WorkspaceExists(s.workspaceID):
			s.role = RoleOwner
			h.audit(p, s.workspaceID, "admin.access "+rt.pattern, r)
		default:
			notFound(w, r)
			return
		}
		if !s.role.Allows(rt.need) {
			writeProblem(w, http.StatusForbidden, "Forbidden", "This action requires the "+string(rt.need)+" role in this workspace.", r.URL.Path)
			return
		}
		rt.handle(h, w, r, s)
	})
}

func (h *studio) admin(rt adminRoute) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := h.principal(r)
		if !p.IsPlatformAdmin {
			writeProblem(w, http.StatusForbidden, "Forbidden", "This endpoint is for platform administrators.", r.URL.Path)
			return
		}
		h.audit(p, "", "admin.read "+rt.pattern, r)
		rt.handle(h, w, r)
	})
}

func (h *studio) adminWrite(rt adminWriteRoute) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := h.principal(r)
		if !p.IsPlatformAdmin {
			writeProblem(w, http.StatusForbidden, "Forbidden", "This endpoint is for platform administrators.", r.URL.Path)
			return
		}
		rt.handle(h, w, r, p)
	})
}

type workspaceAccess struct {
	Workspace
	Role Role `json:"role"`
}

func (h *studio) memberWorkspaces(p *Principal) []workspaceAccess {
	list := []workspaceAccess{}
	for _, ws := range h.store.ListWorkspaces() {
		if role, ok := p.Roles[ws.ID]; ok {
			list = append(list, workspaceAccess{Workspace: *ws, Role: role})
		}
	}
	return list
}

func (h *studio) me(w http.ResponseWriter, r *http.Request) {
	p := h.principal(r)
	claims, _ := auth.FromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"subject":        p.Subject,
		"email":          claims.Email,
		"platform_admin": p.IsPlatformAdmin,
		"workspaces":     h.memberWorkspaces(p),
	})
}

func (h *studio) listMyWorkspaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.memberWorkspaces(h.principal(r)))
}

func (h *studio) listBots(w http.ResponseWriter, r *http.Request, s scope) {
	bots, err := h.store.ListBots(s.workspaceID)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, bots)
}

func (h *studio) createBot(w http.ResponseWriter, r *http.Request, s scope) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Name == "" {
		writeProblem(w, http.StatusBadRequest, "Invalid Request", "name is required", r.URL.Path)
		return
	}
	bot, err := h.store.CreateBot(s.workspaceID, body.Name, body.Description)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.audit(s.principal, s.workspaceID, "bot.created", r)
	writeJSON(w, http.StatusCreated, bot)
}

func (h *studio) getBot(w http.ResponseWriter, r *http.Request, s scope) {
	bot, err := h.store.GetBot(s.workspaceID, r.PathValue("botID"))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, bot)
}

func (h *studio) getActiveVersion(w http.ResponseWriter, r *http.Request, s scope) {
	botID := r.PathValue("botID")
	ver, err := h.store.GetActiveVersion(s.workspaceID, botID)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"bot_id": botID, "active_version": ver})
}

func (h *studio) setActiveVersion(w http.ResponseWriter, r *http.Request, s scope) {
	var body struct {
		Version string `json:"version"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	botID := r.PathValue("botID")
	if err := h.store.SetActiveVersion(s.workspaceID, botID, body.Version); err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.audit(s.principal, s.workspaceID, "bot.active_version_set "+body.Version, r)
	writeJSON(w, http.StatusOK, map[string]string{"bot_id": botID, "active_version": body.Version})
}

func (h *studio) listVersions(w http.ResponseWriter, r *http.Request, s scope) {
	list, err := h.store.ListVersions(s.workspaceID, r.PathValue("botID"))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *studio) createDraft(w http.ResponseWriter, r *http.Request, s scope) {
	var body struct {
		BaseVersion string `json:"base_version"`
	}
	if r.ContentLength != 0 && !decodeBody(w, r, &body) {
		return
	}
	draft, err := h.store.CreateDraft(s.workspaceID, r.PathValue("botID"), body.BaseVersion)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, draft)
}

func (h *studio) getVersion(w http.ResponseWriter, r *http.Request, s scope) {
	ver, err := h.store.GetVersion(s.workspaceID, r.PathValue("botID"), r.PathValue("versionID"))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ver)
}

func (h *studio) saveDraft(w http.ResponseWriter, r *http.Request, s scope) {
	var incoming Version
	if !decodeBody(w, r, &incoming) {
		return
	}
	incoming.BotID = r.PathValue("botID")
	incoming.Version = r.PathValue("versionID")
	if err := h.store.SaveDraft(s.workspaceID, &incoming); err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, incoming)
}

func (h *studio) publish(w http.ResponseWriter, r *http.Request, s scope) {
	botID, versionID := r.PathValue("botID"), r.PathValue("versionID")
	artifact, ver, err := h.store.PublishVersion(s.workspaceID, botID, versionID)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.audit(s.principal, s.workspaceID, "bot.published "+versionID, r)
	writeJSON(w, http.StatusOK, map[string]any{
		"bot_id":       botID,
		"version":      versionID,
		"status":       ver.Status,
		"sha256":       artifact.SHA256,
		"artifact_uri": ver.ArtifactURI,
		"is_active":    true,
	})
}

func (h *studio) listAccess(w http.ResponseWriter, r *http.Request, s scope) {
	access, err := h.store.ListAccess(s.workspaceID)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, access)
}

func (h *studio) setMember(w http.ResponseWriter, r *http.Request, s scope) {
	var body struct {
		Role Role `json:"role"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	subject := r.PathValue("subject")
	if err := h.store.SetMember(s.workspaceID, subject, body.Role); err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.audit(s.principal, s.workspaceID, "member.set "+subject+"="+string(body.Role), r)
	writeJSON(w, http.StatusOK, Member{Subject: subject, Role: body.Role})
}

func (h *studio) removeMember(w http.ResponseWriter, r *http.Request, s scope) {
	subject := r.PathValue("subject")
	if err := h.store.RemoveMember(s.workspaceID, subject); err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.audit(s.principal, s.workspaceID, "member.removed "+subject, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *studio) setGroupGrant(w http.ResponseWriter, r *http.Request, s scope) {
	var body struct {
		DisplayName string `json:"display_name"`
		Role        Role   `json:"role"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	grant := GroupGrant{GroupID: r.PathValue("groupID"), DisplayName: body.DisplayName, Role: body.Role}
	if err := h.store.SetGroupGrant(s.workspaceID, grant); err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.audit(s.principal, s.workspaceID, "group.set "+grant.GroupID+"="+string(grant.Role), r)
	writeJSON(w, http.StatusOK, grant)
}

func (h *studio) removeGroupGrant(w http.ResponseWriter, r *http.Request, s scope) {
	groupID := r.PathValue("groupID")
	if err := h.store.RemoveGroupGrant(s.workspaceID, groupID); err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.audit(s.principal, s.workspaceID, "group.removed "+groupID, r)
	w.WriteHeader(http.StatusNoContent)
}

func (h *studio) adminListWorkspaces(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.ListWorkspaces())
}

func (h *studio) adminCreateWorkspace(w http.ResponseWriter, r *http.Request, p *Principal) {
	var body struct {
		Name    string       `json:"name"`
		Members []Member     `json:"members"`
		Groups  []GroupGrant `json:"groups"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	ws, err := h.store.CreateWorkspace(body.Name, Access{Members: body.Members, Groups: body.Groups})
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	h.audit(p, ws.ID, "workspace.created "+ws.Name, r)
	writeJSON(w, http.StatusCreated, ws)
}

func (h *studio) adminUpdateWorkspace(w http.ResponseWriter, r *http.Request, p *Principal) {
	var body struct {
		Name     *string `json:"name"`
		Archived *bool   `json:"archived"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	workspaceID := r.PathValue("workspaceID")
	ws, err := h.store.UpdateWorkspace(workspaceID, body.Name, body.Archived)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	if body.Name != nil {
		h.audit(p, workspaceID, "workspace.renamed "+ws.Name, r)
	}
	if body.Archived != nil {
		action := "workspace.restored"
		if *body.Archived {
			action = "workspace.archived"
		}
		h.audit(p, workspaceID, action, r)
	}
	writeJSON(w, http.StatusOK, ws)
}

func (h *studio) adminListBots(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.ListAllBots())
}

func (h *studio) adminListAudit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.ListAudit())
}

// devToken mints a token for any subject. It is registered only in local development.
func devToken(iss *auth.DevIssuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Subject string   `json:"subject"`
			Groups  []string `json:"groups"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		if body.Subject == "" {
			writeProblem(w, http.StatusBadRequest, "Invalid Request", "subject is required", r.URL.Path)
			return
		}
		token, err := iss.Mint(body.Subject, body.Groups)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Error", "Could not mint a token.", r.URL.Path)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"access_token": token, "token_type": "Bearer"})
	}
}

// cors allows browser calls only from the configured origins. Preflight requests are
// answered before authentication because browsers never attach credentials to them.
func cors(allowedOrigins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && slices.Contains(allowedOrigins, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
