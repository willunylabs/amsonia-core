package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/postgres"
)

const (
	invoiceRead   amsonia.PermissionKey = "billing:invoice:read"
	invoiceCreate amsonia.PermissionKey = "billing:invoice:create"
)

type demoIdentity struct {
	principal  amsonia.Principal
	org        string
	editor     bool
	accessCode string // random, process-local demo token, never a user password
}

func exampleIdentities() map[string]demoIdentity {
	return map[string]demoIdentity{
		"acme-editor":   {principal: amsonia.Principal{TenantID: "invoice-demo-acme", SubjectID: "acme-editor"}, org: "Acme", editor: true},
		"acme-viewer":   {principal: amsonia.Principal{TenantID: "invoice-demo-acme", SubjectID: "acme-viewer"}, org: "Acme"},
		"globex-editor": {principal: amsonia.Principal{TenantID: "invoice-demo-globex", SubjectID: "globex-editor"}, org: "Globex", editor: true},
	}
}

// These fixed, host-owned demo grants stand in for a production policy store.
type demoPolicy map[string]demoIdentity

func (d demoPolicy) ListEffectiveGrants(_ context.Context, tenant amsonia.TenantID, subject amsonia.SubjectID, permission amsonia.PermissionKey) ([]amsonia.EffectiveGrant, error) {
	identity, ok := d[string(subject)]
	if !ok || identity.principal.TenantID != tenant {
		return nil, nil
	}
	role := amsonia.RoleID("invoice-reader")
	if identity.editor {
		role = "invoice-editor"
	}
	if permission == invoiceRead || (permission == invoiceCreate && identity.editor) {
		return []amsonia.EffectiveGrant{{RoleID: role, Permission: permission, Scope: amsonia.ScopeTenant}}, nil
	}
	return nil, nil
}

type noWorkspaces struct{}

func (noWorkspaces) LookupWorkspaceMembership(context.Context, amsonia.TenantID, amsonia.WorkspaceID, amsonia.SubjectID) (amsonia.WorkspaceMembership, error) {
	return amsonia.WorkspaceMembership{}, amsonia.ErrNotFound
}

type invoiceRepository interface {
	list(context.Context, amsonia.TenantID) ([]invoice, error)
	insert(context.Context, amsonia.TenantID, invoice) error
}

type postgresInvoices struct{ store *postgres.Store }

func (p postgresInvoices) list(ctx context.Context, tenant amsonia.TenantID) ([]invoice, error) {
	return listInvoices(ctx, p.store, tenant)
}

func (p postgresInvoices) insert(ctx context.Context, tenant amsonia.TenantID, item invoice) error {
	return p.store.RunTenant(ctx, tenant, false, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO app.invoices (tenant_id,invoice_id,description,amount_cents)
			VALUES ($1,$2,$3,$4)`, tenant, item.ID, item.Description, item.AmountCents)
		return err
	})
}

type invoiceWeb struct {
	data       invoiceRepository
	authorizer *amsonia.Authorizer
}

type invoicePage struct {
	Org, Description, Amount, Error string
	Form, CanCreate, Saved          bool
	Items                           []invoice
}

//go:embed invoices.html
var invoiceHTML string

var invoiceTemplate = template.Must(template.New("invoices").Funcs(template.FuncMap{
	"dollars": func(cents int64) string { return fmt.Sprintf("$%d.%02d", cents/100, cents%100) },
}).Parse(invoiceHTML))

func (a invoiceWeb) permitted(w http.ResponseWriter, r *http.Request, actor demoIdentity, permission amsonia.PermissionKey) (allowed, ok bool) {
	decision, err := a.authorizer.Check(r.Context(), amsonia.CheckRequest{
		Principal: actor.principal, Permission: permission, Mode: amsonia.ResourceTenantAction,
		Resource: amsonia.ResourceContext{TenantID: actor.principal.TenantID},
	})
	if err != nil {
		a.fail(w, err)
		return false, false
	}
	return decision.Allowed, true
}

func (a invoiceWeb) require(w http.ResponseWriter, r *http.Request, actor demoIdentity, permission amsonia.PermissionKey) bool {
	allowed, ok := a.permitted(w, r, actor, permission)
	if ok && !allowed {
		http.Error(w, "You do not have permission to perform this action.", http.StatusForbidden)
	}
	return ok && allowed
}

func (a invoiceWeb) fail(w http.ResponseWriter, err error) {
	log.Printf("invoice example: %v", err)
	http.Error(w, "The invoice example could not complete the request.", http.StatusInternalServerError)
}

func (a invoiceWeb) render(w http.ResponseWriter, code int, model invoicePage) {
	var result bytes.Buffer
	if err := invoiceTemplate.Execute(&result, model); err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = result.WriteTo(w)
}

func (a invoiceWeb) list(w http.ResponseWriter, r *http.Request, actor demoIdentity) {
	if !a.require(w, r, actor, invoiceRead) {
		return
	}
	canCreate, ok := a.permitted(w, r, actor, invoiceCreate)
	if !ok {
		return
	}
	items, err := a.data.list(r.Context(), actor.principal.TenantID)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, http.StatusOK, invoicePage{
		Org: actor.org, Items: items, CanCreate: canCreate, Saved: r.URL.Query().Get("saved") == "1",
	})
}

func (a invoiceWeb) form(w http.ResponseWriter, r *http.Request, actor demoIdentity) {
	if a.require(w, r, actor, invoiceCreate) {
		a.render(w, http.StatusOK, invoicePage{Org: actor.org, Form: true})
	}
}

func (a invoiceWeb) create(w http.ResponseWriter, r *http.Request, actor demoIdentity) {
	if !a.require(w, r, actor, invoiceCreate) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" || r.URL.RawQuery != "" ||
		r.ParseForm() != nil || len(r.PostForm) != 2 ||
		len(r.PostForm["description"]) != 1 || len(r.PostForm["amount_cents"]) != 1 {
		http.Error(w, "Submit only a description and an amount.", http.StatusBadRequest)
		return
	}
	description := strings.TrimSpace(r.PostForm.Get("description"))
	rawAmount := r.PostForm.Get("amount_cents")
	amount, err := strconv.ParseInt(rawAmount, 10, 64)
	if description == "" || !utf8.ValidString(description) ||
		utf8.RuneCountInString(description) > 120 || strings.ContainsFunc(description, unicode.IsControl) ||
		err != nil || amount < 1 || amount > 99999999 {
		a.render(w, http.StatusUnprocessableEntity, invoicePage{
			Org: actor.org, Form: true, Description: description, Amount: rawAmount,
			Error: "Enter a description of 1–120 characters and an amount of 1–99,999,999 cents.",
		})
		return
	}
	item := invoice{ID: "inv-" + rand.Text(), Description: description, AmountCents: amount}
	if err := a.data.insert(r.Context(), actor.principal.TenantID, item); err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, "/?saved=1", http.StatusSeeOther)
}

func invoiceHandler(host string, identities map[string]demoIdentity, data invoiceRepository) (http.Handler, error) {
	catalog, err := amsonia.NewCatalog([]amsonia.PermissionDefinition{
		{Key: invoiceRead, Description: "List invoices"},
		{Key: invoiceCreate, Description: "Create invoices"},
	})
	if err != nil {
		return nil, err
	}
	authorizer, err := amsonia.NewAuthorizer(catalog, demoPolicy(identities), noWorkspaces{})
	if err != nil {
		return nil, err
	}
	app := invoiceWeb{data: data, authorizer: authorizer}
	mux := http.NewServeMux()
	authenticated := func(next func(http.ResponseWriter, *http.Request, demoIdentity)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			user, code, basic := r.BasicAuth()
			actor, found := identities[user]
			if !basic || !found || subtle.ConstantTimeCompare([]byte(code), []byte(actor.accessCode)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="Amsonia invoice example"`)
				http.Error(w, "Use the credentials printed by the local example.", http.StatusUnauthorized)
				return
			}
			next(w, r, actor)
		}
	}
	mux.HandleFunc("GET /{$}", authenticated(app.list))
	mux.HandleFunc("GET /invoices/new", authenticated(app.form))
	mux.HandleFunc("POST /invoices", authenticated(app.create))
	csrf := http.NewCrossOriginProtection().Handler(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Host != host {
			http.Error(w, "Open the exact loopback URL printed by the example.", http.StatusBadRequest)
			return
		}
		csrf.ServeHTTP(w, r)
	}), nil
}

func serveInvoices(store *postgres.Store) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	host := listener.Addr().String()
	identities := exampleIdentities()
	fmt.Printf("Invoice example: http://%s/ (local development only)\n", host)
	for _, name := range []string{"acme-editor", "acme-viewer", "globex-editor"} {
		secret := make([]byte, 24)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
		code := base64.RawURLEncoding.EncodeToString(secret)
		actor := identities[name]
		actor.accessCode = code
		identities[name] = actor
		fmt.Printf("  %s: %s\n", name, code)
	}
	handler, err := invoiceHandler(host, identities, postgresInvoices{store})
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192,
	}
	return server.Serve(listener)
}
