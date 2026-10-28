package pagination

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestBoundsAndCursorScope(t *testing.T) {
	binding := Binding(url.Values{}, "purchases.list", "alice", "tenant")

	for _, input := range []string{"limit=0", "limit=101", "limit=-1", "limit=+1", "limit=1.5", "limit=1&limit=2", "limit=", "cursor=", "cursor=%%%", "cursor=" + strings.Repeat("x", MaxCursor+1)} {
		query, err := url.ParseQuery(input)
		if err != nil {
			query = url.Values{"cursor": {"!"}}
		}

		if _, err := Parse(query, binding); err == nil {
			t.Errorf("accepted %q", input)
		}
	}

	request, err := Parse(nil, binding)
	if err != nil || request.Limit != 25 || request.FetchLimit() != 26 {
		t.Fatalf("default: %+v %v", request, err)
	}

	request, err = Parse(url.Values{"limit": {"100"}}, binding)
	if err != nil || request.FetchLimit() != 101 {
		t.Fatal(request, err)
	}

	request, _ = Parse(url.Values{"limit": {"2"}}, binding)
	rows := []Key{
		{Sort: "same", ID: "a"},
		{Sort: "same", ID: "b"},
		{Sort: "same", ID: "c"},
	}

	page, err := Finish(request, rows, func(k Key) Key { return k })
	if err != nil || !page.Page.HasMore || len(page.Data) != 2 {
		t.Fatal(page, err)
	}

	next, err := Parse(url.Values{"cursor": {page.Page.NextCursor}}, binding)
	if err != nil || next.After != rows[1] {
		t.Fatal(next, err)
	}

	for _, scope := range [][]string{{"other", "alice", "tenant"}, {"purchases.list", "bob", "tenant"}, {"purchases.list", "alice", "other"}} {
		if _, err := Parse(
			url.Values{"cursor": {page.Page.NextCursor}},
			Binding(nil, scope...),
		); err == nil {
			t.Fatal("cross scope cursor accepted")
		}
	}

	if _, err := Parse(
		url.Values{"cursor": {page.Page.NextCursor}},
		Binding(
			url.Values{"filter": {"x"}},
			"purchases.list",
			"alice",
			"tenant",
		),
	); err == nil {
		t.Fatal("cross filter cursor")
	}

	final, err := Finish(next, rows[2:], func(k Key) Key { return k })
	if err != nil || final.Page.HasMore || final.Page.NextCursor != "" ||
		len(final.Data) != 1 {
		t.Fatal(final, err)
	}

	empty, err := Finish(next, []Key(nil), func(k Key) Key { return k })
	if err != nil || empty.Data == nil || empty.Page.HasMore {
		t.Fatal(empty, err)
	}

	if _, err := Finish(
		request,
		append(rows, Key{ID: "d"}),
		func(k Key) Key { return k },
	); err == nil {
		t.Fatal("unbounded rows accepted")
	}

	for _, raw := range []string{`{"v":2,"b":"` + binding + `","k":{"id":"a","sort":""}}`, `{"v":1,"b":"` + binding + `","k":{"id":"","sort":""}}`, `{}`, `{} {}`} {
		if _, err := Parse(
			url.Values{
				"cursor": {base64.RawURLEncoding.EncodeToString([]byte(raw))},
			},
			binding,
		); err == nil {
			t.Fatal("malformed version/key")
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"", "limit=100", "cursor=bad", "limit=101", "limit=1&limit=2"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 8192 {
			t.Skip()
		}

		query, err := url.ParseQuery(raw)
		if err != nil {
			return
		}

		request, err := Parse(query, Binding(query, "list", "verified"))
		if err == nil &&
			(request.Validate() != nil || request.FetchLimit() > 101) {
			t.Fatal("unbounded pagination")
		}
	})
}

func TestEncodedCursorCannotExceedBound(t *testing.T) {
	request, _ := Parse(
		url.Values{"limit": {"1"}},
		Binding(nil, "list", "verified"),
	)

	rows := []Key{
		{Sort: strings.Repeat("\x00", 256), ID: strings.Repeat("\x00", 256)},
		{ID: "lookahead"},
	}
	if page, err := Finish(
		request,
		rows,
		func(k Key) Key { return k },
	); err == nil {
		t.Fatal("emitted oversized escaped cursor", page)
	}
}
