package logging

import "testing"

func TestNoOpLogger_AllMethods(t *testing.T) {
	l := &NoOpLogger{}
	l.Info("test info", nil)
	l.Info("test info with fields", map[string]interface{}{"key": "value"})
	l.Warn("test warn", nil)
	l.Warn("test warn with fields", map[string]interface{}{"key": "value"})
	l.Error("test error", nil)
	l.Error("test error with fields", map[string]interface{}{"key": "value"})
}

func TestNoOpLogger_NilFields(t *testing.T) {
	l := &NoOpLogger{}
	l.Info("msg", nil)
}

type capturingLogger struct {
	entries []entry
}

type entry struct {
	level  string
	msg    string
	fields map[string]interface{}
}

func (c *capturingLogger) Info(msg string, fields map[string]interface{}) {
	c.entries = append(c.entries, entry{level: "INFO", msg: msg, fields: fields})
}

func (c *capturingLogger) Warn(msg string, fields map[string]interface{}) {
	c.entries = append(c.entries, entry{level: "WARN", msg: msg, fields: fields})
}

func (c *capturingLogger) Error(msg string, fields map[string]interface{}) {
	c.entries = append(c.entries, entry{level: "ERROR", msg: msg, fields: fields})
}

func TestCapturingLogger_Info(t *testing.T) {
	c := &capturingLogger{}
	c.Info("test info", map[string]interface{}{"key": "val"})
	if len(c.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(c.entries))
	}
	if c.entries[0].msg != "test info" {
		t.Fatalf("expected msg 'test info', got %s", c.entries[0].msg)
	}
	if c.entries[0].level != "INFO" {
		t.Fatalf("expected level INFO, got %s", c.entries[0].level)
	}
	if c.entries[0].fields["key"] != "val" {
		t.Fatalf("expected field key=val, got %v", c.entries[0].fields["key"])
	}
}

func TestCapturingLogger_Warn(t *testing.T) {
	c := &capturingLogger{}
	c.Warn("test warn", nil)
	if len(c.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(c.entries))
	}
	if c.entries[0].msg != "test warn" {
		t.Fatalf("expected msg 'test warn', got %s", c.entries[0].msg)
	}
	if c.entries[0].level != "WARN" {
		t.Fatalf("expected level WARN, got %s", c.entries[0].level)
	}
}

func TestCapturingLogger_Error(t *testing.T) {
	c := &capturingLogger{}
	c.Error("test error", map[string]interface{}{"code": 500})
	if len(c.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(c.entries))
	}
	if c.entries[0].msg != "test error" {
		t.Fatalf("expected msg 'test error', got %s", c.entries[0].msg)
	}
	if c.entries[0].level != "ERROR" {
		t.Fatalf("expected level ERROR, got %s", c.entries[0].level)
	}
	if c.entries[0].fields["code"] != 500 {
		t.Fatalf("expected field code=500, got %v", c.entries[0].fields["code"])
	}
}

func TestCapturingLogger_MultipleEntries(t *testing.T) {
	c := &capturingLogger{}
	c.Info("one", nil)
	c.Warn("two", nil)
	c.Error("three", nil)
	if len(c.entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(c.entries))
	}
	if c.entries[0].msg != "one" || c.entries[1].msg != "two" || c.entries[2].msg != "three" {
		t.Fatalf("entries mismatch: %v", c.entries)
	}
}

func TestCapturingLogger_NilFields(t *testing.T) {
	c := &capturingLogger{}
	c.Info("msg", nil)
	if c.entries[0].fields != nil {
		t.Fatalf("expected nil fields, got %v", c.entries[0].fields)
	}
}
