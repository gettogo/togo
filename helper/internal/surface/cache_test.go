package surface

import (
	"os"
	"path/filepath"
	"testing"
)

func testCache(t *testing.T, root string) *Cache {
	t.Helper()
	c, err := OpenCache(root, CacheIdentity{Generation: "run", Birth: "process", Conversation: "chat"}, testScene().Options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func cacheApply(t *testing.T, c *Cache, verb byte, body string) {
	t.Helper()
	if _, err := c.Apply(sceneMessage(verb, body)); err != nil {
		t.Fatal(err)
	}
}

func cacheText(t *testing.T, c *Cache) string {
	t.Helper()
	b, _, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	s, err := RestoreScene(b, testScene().Options)
	if err != nil {
		t.Fatal(err)
	}
	return s.Views[0].Document.nodes["text"].node.Props["text"].(string)
}

func TestCacheReplaysSyncedFramesAndDropsOnlyAnIncompleteTail(t *testing.T) {
	root := t.TempDir()
	c := testCache(t, root)
	cacheApply(t, c, 'o', `{"id":"one"}`)
	cacheApply(t, c, 'f', `{"sf":"one","s":1,"ops":[["add","text","one",null,{"id":"text","k":"text","p":{"text":"A"}}]]}`)
	cacheApply(t, c, 'f', `{"sf":"one","s":2,"ops":[["text","text","append","B"]]}`)
	c.Close()
	f, err := os.OpenFile(filepath.Join(root, "frames.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"revision":4,"verb":`)
	f.Close()
	next := testCache(t, root)
	if got := cacheText(t, next); got != "AB" {
		t.Fatalf("restart lost or duplicated text: %q", got)
	}
	cacheApply(t, next, 'f', `{"sf":"one","s":3,"ops":[["text","text","append","C"]]}`)
	next.Close()
	if got := cacheText(t, testCache(t, root)); got != "ABC" {
		t.Fatalf("recovery did not remove incomplete tail: %q", got)
	}
}

func TestCacheCheckpointCannotReplayAlreadySnapshottedText(t *testing.T) {
	root := t.TempDir()
	c := testCache(t, root)
	cacheApply(t, c, 'o', `{"id":"one"}`)
	cacheApply(t, c, 'f', `{"sf":"one","s":1,"ops":[["add","text","one",null,{"id":"text","k":"text","p":{"text":"A"}}]]}`)
	cacheApply(t, c, 'f', `{"sf":"one","s":2,"ops":[["text","text","append","B"]]}`)
	old, err := os.ReadFile(filepath.Join(root, "frames.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	c.maxJournal = 1
	cacheApply(t, c, 'f', `{"sf":"one","s":3,"ops":[["set","text",{"tone":"muted"}]]}`)
	c.Close()
	// A crash after snapshot replacement but before journal truncation.
	os.WriteFile(filepath.Join(root, "frames.jsonl"), old, 0o600)
	if got := cacheText(t, testCache(t, root)); got != "AB" {
		t.Fatalf("old journal replayed a text append twice: %q", got)
	}
}

func TestCacheRejectsAnotherRuntimeAndCorruptedCompleteRecord(t *testing.T) {
	root := t.TempDir()
	c := testCache(t, root)
	c.Close()
	if _, err := OpenCache(root, CacheIdentity{Generation: "other"}, testScene().Options); err == nil {
		t.Fatal("another runtime adopted the document cache")
	}
	os.WriteFile(filepath.Join(root, "frames.jsonl"), []byte("{broken}\n"), 0o600)
	if _, err := OpenCache(root, CacheIdentity{Generation: "run", Birth: "process", Conversation: "chat"}, testScene().Options); err == nil {
		t.Fatal("corrupted complete journal record was ignored")
	}
}
