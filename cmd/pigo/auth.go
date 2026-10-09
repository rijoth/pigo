package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"pigo/internal/auth"
	"pigo/internal/event"
	"pigo/internal/providers"
	"pigo/internal/ui"
)

// handleLogin implements /login. With an empty arg it opens the provider
// picker; otherwise it uses the named provider directly. rebuild is called
// with the provider id after a successful login so the active model can pick
// up the new key.
func handleLogin(u *ui.UI, arg, authPath string, rebuild func(string)) {
	plist := providers.Providers()
	creds, err := auth.Load(authPath)
	if err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return
	}
	if arg == "" && !u.Interactive() {
		printProviders(os.Stdout, plist, creds)
		return
	}
	if arg == "" {
		items := make([]ui.SelectItem, 0, len(plist))
		for _, p := range plist {
			items = append(items, ui.SelectItem{Value: p.ID, Label: providerLabel(p, creds)})
		}
		ref, selErr := u.Select("provider", items, "")
		if errors.Is(selErr, ui.ErrCancel) {
			return
		}
		if selErr != nil {
			u.Render(event.Event{Kind: event.Error, Err: selErr})
			return
		}
		arg = ref
	}
	p, ok := providers.FindProvider(arg)
	if !ok {
		u.Render(event.Event{Kind: event.Error, Err: fmt.Errorf("unknown provider %q", arg)})
		return
	}
	if p.OAuth {
		u.Render(event.Event{Kind: event.Notice, Text: p.ID + ": not supported, use an API key"})
		return
	}
	key, err := u.ReadSecret("API key for " + p.Name + ": ")
	if err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return
	}
	if key == "" {
		u.Render(event.Event{Kind: event.Error, Err: errors.New("API key must not be empty")})
		return
	}
	if err := auth.Set(authPath, p.ID, key); err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return
	}
	u.Render(event.Event{Kind: event.Notice, Text: "logged in to " + p.ID})
	rebuild(p.ID)
}

// handleLogout implements /logout: pick one provider with a stored credential
// and remove it.
func handleLogout(u *ui.UI, authPath string) {
	creds, err := auth.Load(authPath)
	if err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return
	}
	ids := make([]string, 0, len(creds))
	for id := range creds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		u.Render(event.Event{Kind: event.Notice, Text: "no stored credentials"})
		return
	}
	if !u.Interactive() {
		for _, id := range ids {
			fmt.Fprintln(os.Stdout, id)
		}
		return
	}
	items := make([]ui.SelectItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, ui.SelectItem{Value: id, Label: id})
	}
	ref, selErr := u.Select("logout", items, "")
	if errors.Is(selErr, ui.ErrCancel) {
		return
	}
	if selErr != nil {
		u.Render(event.Event{Kind: event.Error, Err: selErr})
		return
	}
	if err := auth.Delete(authPath, ref); err != nil {
		u.Render(event.Event{Kind: event.Error, Err: err})
		return
	}
	u.Render(event.Event{Kind: event.Notice, Text: "logged out from " + ref})
}

// providerLabel renders the status marker shown next to a provider.
func providerLabel(p providers.ProviderInfo, creds map[string]auth.Credential) string {
	switch {
	case p.OAuth:
		return p.ID + " — not supported, use an API key"
	case creds[p.ID].Key != "":
		return p.ID + " — logged in"
	case p.EnvVar != "" && os.Getenv(p.EnvVar) != "":
		return p.ID + " — env " + p.EnvVar
	default:
		return p.ID + " — not configured"
	}
}

func printProviders(w io.Writer, providersList []providers.ProviderInfo, creds map[string]auth.Credential) {
	for _, p := range providersList {
		fmt.Fprintf(w, "%-22s %s\n", p.ID, providerLabel(p, creds))
	}
}

// providerOf extracts the provider id from a "provider/model" reference.
func providerOf(ref string) string {
	if i := strings.IndexByte(ref, '/'); i > 0 {
		return ref[:i]
	}
	return ref
}
