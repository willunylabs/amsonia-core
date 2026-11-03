package main

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/willunylabs/amsonia-core"
)

type testInvoices struct {
	items map[amsonia.TenantID][]invoice
}

func (t testInvoices) list(_ context.Context, tenant amsonia.TenantID) ([]invoice, error) {
	return t.items[tenant], nil
}

func (t testInvoices) insert(_ context.Context, tenant amsonia.TenantID, item invoice) error {
	t.items[tenant] = append(t.items[tenant], item)
	return nil
}

func TestInvoiceFormAuthorityAndPersistence(t *testing.T) {
	ids := exampleIdentities()
	for name, identity := range ids {
		identity.password = sha256.Sum256([]byte("test-" + name))
		ids[name] = identity
	}
	repo := testInvoices{items: map[amsonia.TenantID][]invoice{}}
	handler, err := invoiceHandler("127.0.0.1:1234", ids, repo)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, user, body, origin string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:1234"+path, strings.NewReader(body))
		if user != "" {
			r.SetBasicAuth(user, "test-"+user)
		}
		if method == "POST" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s as %s: status %d, want %d: %s", method, path, user, w.Code, want, w.Body)
		}
		return w
	}
	const valid = "description=Annual+service&amount_cents=1250"
	request("GET", "/", "", "", "", 401)
	request("GET", "/invoices/new", "acme-viewer", "", "", 403)
	request("POST", "/invoices", "acme-viewer", valid, "", 403)
	request("POST", "/invoices", "acme-editor", valid, "https://elsewhere.example", 403)
	request("POST", "/invoices", "acme-editor", valid+"&tenant_id=invoice-demo-globex", "", 400)
	request("POST", "/invoices?tenant_id=invoice-demo-globex", "acme-editor", valid, "", 400)
	request("POST", "/invoices", "acme-editor", valid+"&amount_cents=7", "", 400)
	bad := request("POST", "/invoices", "acme-editor", "description=Kept+draft&amount_cents=0", "", 422)
	if !strings.Contains(bad.Body.String(), "Kept draft") || len(repo.items) != 0 {
		t.Fatal("invalid or unauthorized input reached storage or lost the draft")
	}
	form := request("GET", "/invoices/new", "acme-editor", "", "", 200)
	if !strings.Contains(form.Body.String(), `method="post" action="/invoices"`) {
		t.Fatal("create route does not expose a form")
	}
	saved := request("POST", "/invoices", "acme-editor", valid, "http://127.0.0.1:1234", 303)
	if saved.Header().Get("Location") != "/?saved=1" {
		t.Fatal("missing post-redirect-get location")
	}
	rows := repo.items["invoice-demo-acme"]
	if len(rows) != 1 || rows[0].AmountCents != 1250 || rows[0].ID == "" {
		t.Fatalf("wrong storage scope or contents: %+v", repo.items)
	}
	reader := request("GET", "/", "acme-viewer", "", "", 200).Body.String()
	if !strings.Contains(reader, "Annual service") || strings.Contains(reader, `href="/invoices/new"`) {
		t.Fatal("viewer cannot read or was offered edit controls")
	}
	other := request("GET", "/", "globex-editor", "", "", 200).Body.String()
	if strings.Contains(other, "Annual service") {
		t.Fatal("cross-organization invoice leak")
	}
	r := httptest.NewRequest(http.MethodGet, "http://rebinding.example/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("untrusted Host accepted")
	}
}
