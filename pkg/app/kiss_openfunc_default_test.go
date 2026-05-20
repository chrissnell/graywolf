//go:build !android

package app

import "testing"

func TestNewKissSerialOpenFunc_NonAndroid_IsNil(t *testing.T) {
	if got := newKissSerialOpenFunc(nil); got != nil {
		t.Fatalf("expected nil on non-Android; got non-nil")
	}
}

func TestApp_KissSerialOpenFunc_NonAndroid_IsNil(t *testing.T) {
	a := &App{}
	if got := a.kissSerialOpenFunc(); got != nil {
		t.Fatalf("expected nil on non-Android; got non-nil")
	}
}
