package rediscache

import (
	"testing"
	"time"
)

func TestOptionsFromURLAppliesBoundedTimeouts(t *testing.T) {
	options, err := optionsFromURL("redis://127.0.0.1:6379/2")
	if err != nil {
		t.Fatal(err)
	}
	if options.Addr != "127.0.0.1:6379" || options.DB != 2 {
		t.Fatalf("Redis options = addr %q db %d", options.Addr, options.DB)
	}
	if options.DialTimeout != 500*time.Millisecond ||
		options.ReadTimeout != 250*time.Millisecond ||
		options.WriteTimeout != 250*time.Millisecond {
		t.Fatalf("unexpected Redis timeouts: dial=%s read=%s write=%s",
			options.DialTimeout, options.ReadTimeout, options.WriteTimeout)
	}
}

func TestOptionsFromURLRejectsNonRedisScheme(t *testing.T) {
	if _, err := optionsFromURL("https://example.com"); err == nil {
		t.Fatal("non-Redis URL was accepted")
	}
}
