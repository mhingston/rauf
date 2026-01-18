package main

import (
	"testing"
)

func TestLimitedBuffer(t *testing.T) {
	t.Run("zero max", func(t *testing.T) {
		lb := &limitedBuffer{max: 0}
		n, err := lb.Write([]byte("hello"))
		if err != nil {
			t.Fatal(err)
		}
		if n != 5 {
			t.Errorf("got n=%d, want 5", n)
		}
		if lb.String() != "" {
			t.Errorf("got %q, want empty", lb.String())
		}
	})

	t.Run("less than max", func(t *testing.T) {
		lb := &limitedBuffer{max: 10}
		lb.Write([]byte("abc"))
		if lb.String() != "abc" {
			t.Errorf("got %q, want abc", lb.String())
		}
	})

	t.Run("exactly max", func(t *testing.T) {
		lb := &limitedBuffer{max: 5}
		lb.Write([]byte("12345"))
		if lb.String() != "12345" {
			t.Errorf("got %q, want 12345", lb.String())
		}
	})

	t.Run("larger than max at once", func(t *testing.T) {
		lb := &limitedBuffer{max: 3}
		lb.Write([]byte("abcdef"))
		if lb.String() != "def" {
			t.Errorf("got %q, want def", lb.String())
		}
	})

	t.Run("overflow over multiple writes", func(t *testing.T) {
		lb := &limitedBuffer{max: 5}
		lb.Write([]byte("abc"))
		lb.Write([]byte("def")) // total 6, should drop 'a'
		if lb.String() != "bcdef" {
			t.Errorf("got %q, want bcdef", lb.String())
		}
	})

	t.Run("large overflow", func(t *testing.T) {
		lb := &limitedBuffer{max: 2}
		lb.Write([]byte("a"))
		lb.Write([]byte("bcdef")) // total 6, should drop everything except 'ef'
		if lb.String() != "ef" {
			t.Errorf("got %q, want ef", lb.String())
		}
	})
}
