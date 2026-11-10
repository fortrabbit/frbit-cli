package backups

import "testing"

func TestExtension(t *testing.T) {
	for _, c := range []struct{ disposition, urlPath, want string }{
		{`attachment; filename="../../x.zip"`, "/a/b.sql", ".zip"},
		{`attachment; filename="/etc/passwd"`, "/a/b.zip", ""},
		{`attachment; filename="a\..\b.tar.gz"`, "/a/b.zip", ".tar.gz"},
		{"", "/storage/site.zip", ".zip"},
		{"", "/storage/db.sql.gz", ".sql.gz"},
		{"attachment; filename=\"a.b\x1b[2Jc\"", "/a/b.zip", ""},
		{`attachment; filename="a.z;ip"`, "/a/b.zip", ""},
	} {
		if got := extension(c.disposition, c.urlPath); got != c.want {
			t.Errorf("extension(%q, %q) = %q, want %q", c.disposition, c.urlPath, got, c.want)
		}
	}
}
