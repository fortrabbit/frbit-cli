package db

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type sshCall struct {
	remote string
	stdin  []byte
}

// pushRunner records every ssh call and fails the call whose remote command contains a key of fail.
type pushRunner struct {
	calls  []sshCall
	fail   map[string]error
	onCall func(remote string)
}

func (f *pushRunner) Run(_ context.Context, name string, args []string, stdin io.Reader, _, _ io.Writer) error {
	if name != "ssh" {
		return errors.New("not ssh: " + name)
	}
	call := sshCall{remote: args[len(args)-1]}
	if stdin != nil {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		call.stdin = data
	}
	f.calls = append(f.calls, call)
	if f.onCall != nil {
		f.onCall(call.remote)
	}
	for key, err := range f.fail {
		if strings.Contains(call.remote, key) {
			return err
		}
	}
	return nil
}

func (f *pushRunner) remotes() []string {
	list := []string{}
	for _, call := range f.calls {
		list = append(list, call.remote)
	}
	return list
}

const (
	wantBackup  = `set -o pipefail; mysqldump --single-transaction --set-gtid-purged=OFF --no-tablespaces "$FORTRABBIT_DB_NAME" | gzip > /data/home/frbit-backup-20261110093005.sql.gz || { status=$?; rm -f /data/home/frbit-backup-20261110093005.sql.gz; exit $status; }`
	wantUpload  = `rm -f /data/home/fortrabbit-import-*.sql.gz; cat > /data/home/frbit-import.sql.gz`
	wantTest    = `gzip -t /data/home/frbit-import.sql.gz`
	wantImport  = `set -o pipefail; gunzip -c /data/home/frbit-import.sql.gz | mysql "$FORTRABBIT_DB_NAME"`
	wantCleanup = `rm -f /data/home/frbit-import.sql.gz`
	wantRestore = `ssh en-abc123@ssh.us-e1a.frbit.app 'set -o pipefail; gunzip -c /data/home/frbit-backup-20261110093005.sql.gz | mysql "$FORTRABBIT_DB_NAME"'`
)

func push(t *testing.T, dir string, fake *pushRunner, in invocation) (string, string, error) {
	t.Helper()
	previous := now
	now = func() time.Time { return time.Date(2026, 11, 10, 10, 30, 5, 0, time.FixedZone("CET", 3600)) }
	t.Cleanup(func() { now = previous })
	in.runner = fake
	in.check = func(string) error { return nil }
	in.args = append([]string{"db", "push"}, in.args...)
	return execute(t, dir, in)
}

func writeFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDbPushWithoutTTYRequiresConfirmBeforeSSH(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dump.sql", []byte(dump))
	for _, args := range [][]string{
		{"dump.sql", "en-abc123"},
		{"dump.sql", "en-abc123", "--confirm", "en-zzz999"},
	} {
		fake := &pushRunner{}
		requests := []string{}
		_, _, err := push(t, dir, fake, invocation{requests: &requests, args: args})
		if err == nil || !strings.Contains(err.Error(), "--confirm") {
			t.Fatalf("%v: %v", args, err)
		}
		if len(fake.calls) != 0 || len(requests) != 0 {
			t.Fatalf("%v: ssh %q, requests %q", args, fake.remotes(), requests)
		}
	}

	// stdin carries the dump, so it cannot answer a prompt: - needs --confirm.
	fake := &pushRunner{}
	_, _, err := push(t, dir, fake, invocation{stdin: strings.NewReader(dump), args: []string{"-", "en-abc123"}})
	if err == nil || !strings.Contains(err.Error(), "--confirm en-abc123") || len(fake.calls) != 0 {
		t.Fatalf("stdin without --confirm: %v, %q", err, fake.remotes())
	}
}

func TestDbPushStdinOnTTYRefusedBeforeReading(t *testing.T) {
	dir := t.TempDir()
	fake := &pushRunner{}
	requests := []string{}
	stdin := strings.NewReader(dump)
	_, _, err := push(t, dir, fake, invocation{tty: true, stdin: stdin, requests: &requests, args: []string{"-", "en-abc123", "--confirm", "en-abc123"}})
	if err == nil || err.Error() != "- reads the dump from stdin; pipe a file in" {
		t.Fatalf("err = %v", err)
	}
	if stdin.Len() != len(dump) || len(fake.calls) != 0 || len(requests) != 0 {
		t.Fatalf("read %d bytes, ssh %q, requests %q", len(dump)-stdin.Len(), fake.remotes(), requests)
	}
}

func TestDbPushPromptsOnTTY(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dump.sql", []byte(dump))
	fake := &pushRunner{}
	_, stderr, err := push(t, dir, fake, invocation{tty: true, stdin: strings.NewReader("nope\n"), args: []string{"dump.sql", "en-abc123"}})
	if err != nil || len(fake.calls) != 0 || !strings.Contains(stderr, "Aborted.") {
		t.Fatalf("declined: %v, %q, %q", err, fake.remotes(), stderr)
	}

	fake = &pushRunner{}
	_, stderr, err = push(t, dir, fake, invocation{tty: true, stdin: strings.NewReader("en-abc123\n"), args: []string{"dump.sql", "en-abc123"}})
	if err != nil || len(fake.calls) != 5 {
		t.Fatalf("confirmed: %v, %q", err, fake.remotes())
	}
	for _, want := range []string{"WARNING:", "en-abc123", "replaces the database", "backup", "Type en-abc123 to confirm:"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q: %q", want, stderr)
		}
	}
}

func TestDbPushStepsInOrder(t *testing.T) {
	dir := t.TempDir()
	plain := writeFile(t, dir, "dump.sql", []byte(dump))
	packed := writeFile(t, dir, "dump.sql.gz", gzipped(t, dump))
	for _, in := range []invocation{
		{args: []string{plain, "en-abc123", "--confirm", "en-abc123"}},
		{args: []string{packed, "en-abc123", "--confirm", "en-abc123"}},
		{stdin: strings.NewReader(dump), args: []string{"-", "en-abc123", "--confirm", "en-abc123"}},
		{stdin: bytes.NewReader(gzipped(t, dump)), args: []string{"-", "en-abc123", "--confirm", "en-abc123"}},
	} {
		fake := &pushRunner{}
		stdout, stderr, err := push(t, dir, fake, in)
		if err != nil {
			t.Fatalf("%v: %v\n%s", in.args, err, stderr)
		}
		if got := fake.remotes(); !slices.Equal(got, []string{wantBackup, wantUpload, wantTest, wantImport, wantCleanup}) {
			t.Fatalf("%v: ran\n%s", in.args, strings.Join(got, "\n"))
		}
		var sql bytes.Buffer
		if _, err := decompress(&sql, bytes.NewReader(fake.calls[1].stdin), -1); err != nil || sql.String() != dump {
			t.Fatalf("%v: uploaded %q, %v", in.args, sql.String(), err)
		}
		if stdout != "" {
			t.Fatalf("stdout %q", stdout)
		}
		if !strings.Contains(stderr, "/data/home/frbit-backup-20261110093005.sql.gz") {
			t.Fatalf("stderr %q", stderr)
		}
	}
}

func TestDbPushRefusesBadInputBeforeSSH(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "empty.sql", nil)
	writeFile(t, dir, "fake.sql.gz", []byte(dump))
	broken := gzipped(t, dump)
	writeFile(t, dir, "broken.sql.gz", broken[:len(broken)-6])
	for _, in := range []invocation{
		{args: []string{"empty.sql", "en-abc123", "--confirm", "en-abc123"}},
		{args: []string{"missing.sql", "en-abc123", "--confirm", "en-abc123"}},
		{args: []string{"fake.sql.gz", "en-abc123", "--confirm", "en-abc123"}},
		{args: []string{"broken.sql.gz", "en-abc123", "--confirm", "en-abc123"}},
		{stdin: strings.NewReader(""), args: []string{"-", "en-abc123", "--confirm", "en-abc123"}},
	} {
		fake := &pushRunner{}
		requests := []string{}
		in.requests = &requests
		if _, _, err := push(t, dir, fake, in); err == nil {
			t.Fatalf("%v: no error", in.args)
		}
		if len(fake.calls) != 0 || len(requests) != 0 {
			t.Fatalf("%v: ssh %q, requests %q", in.args, fake.remotes(), requests)
		}
	}
}

func TestDbPushFailedBackupLeavesDatabase(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dump.sql", []byte(dump))
	fake := &pushRunner{fail: map[string]error{"mysqldump": exitError(2)}}
	_, _, err := push(t, dir, fake, invocation{args: []string{"dump.sql", "en-abc123", "--confirm", "en-abc123"}})
	if err == nil || !strings.Contains(err.Error(), "backup") || !strings.Contains(err.Error(), "not changed") {
		t.Fatalf("backup failure: %v", err)
	}
	if got := fake.remotes(); !slices.Equal(got, []string{wantBackup}) {
		t.Fatalf("ran %q", got)
	}
}

func TestDbPushFailedImportNamesBackup(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dump.sql", []byte(dump))
	fake := &pushRunner{fail: map[string]error{"| mysql": exitError(1)}}
	_, stderr, err := push(t, dir, fake, invocation{args: []string{"dump.sql", "en-abc123", "--confirm", "en-abc123"}})
	if err == nil || !strings.Contains(err.Error(), "import") {
		t.Fatalf("import failure: %v", err)
	}
	if got := fake.remotes(); !slices.Equal(got, []string{wantBackup, wantUpload, wantTest, wantImport, wantCleanup}) {
		t.Fatalf("ran %q", got)
	}
	for _, want := range []string{"partly", "/data/home/frbit-backup-20261110093005.sql.gz", wantRestore} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q: %q", want, stderr)
		}
	}
}

func TestDbPushDamagedUploadImportsNothing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dump.sql", []byte(dump))
	fake := &pushRunner{fail: map[string]error{"gzip -t": exitError(1)}}
	_, stderr, err := push(t, dir, fake, invocation{args: []string{"dump.sql", "en-abc123", "--confirm", "en-abc123"}})
	if err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("damaged: %v", err)
	}
	if got := fake.remotes(); !slices.Equal(got, []string{wantBackup, wantUpload, wantTest, wantCleanup}) {
		t.Fatalf("ran %q", got)
	}
	if !strings.Contains(stderr, "not changed") || !strings.Contains(stderr, wantRestore) {
		t.Fatalf("stderr %q", stderr)
	}
}

func TestDbPushSSHKeyRejected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dump.sql", []byte(dump))
	fake := &pushRunner{fail: map[string]error{"mysqldump": exitError(255)}}
	_, _, err := push(t, dir, fake, invocation{args: []string{"dump.sql", "en-abc123", "--confirm", "en-abc123"}})
	if err == nil || !strings.Contains(err.Error(), "https://dash.fortrabbit.com/new/ssh-key") {
		t.Fatalf("key rejected: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("ran %q", fake.remotes())
	}
}

func TestDbPushCtrlCDuringUploadRemovesImportFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dump.sql", []byte(dump))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &pushRunner{fail: map[string]error{"cat >": errors.New("signal: killed")}, onCall: func(remote string) {
		if strings.Contains(remote, "cat >") {
			cancel()
		}
	}}
	_, stderr, err := push(t, dir, fake, invocation{ctx: ctx, args: []string{"dump.sql", "en-abc123", "--confirm", "en-abc123"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Ctrl-C: %v", err)
	}
	if got := fake.remotes(); !slices.Equal(got, []string{wantBackup, wantUpload, wantCleanup}) {
		t.Fatalf("ran %q", got)
	}
	if !strings.Contains(stderr, "not changed") {
		t.Fatalf("stderr %q", stderr)
	}
}

func TestDbPushCtrlCDuringImportSaysPartial(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dump.sql", []byte(dump))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake := &pushRunner{fail: map[string]error{"| mysql": errors.New("signal: killed")}, onCall: func(remote string) {
		if strings.Contains(remote, "| mysql") {
			cancel()
		}
	}}
	_, stderr, err := push(t, dir, fake, invocation{ctx: ctx, args: []string{"dump.sql", "en-abc123", "--confirm", "en-abc123"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Ctrl-C: %v", err)
	}
	if !strings.Contains(stderr, "partly") || !strings.Contains(stderr, wantRestore) {
		t.Fatalf("stderr %q", stderr)
	}
}
