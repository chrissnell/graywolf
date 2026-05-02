package configstore

import (
	"context"
	"testing"
)

func TestListenerAddresseeCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateActionListenerAddressee(ctx, "gwact"); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListActionListenerAddressees(ctx)
	if err != nil || len(all) != 1 || all[0].Addressee != "GWACT" {
		t.Fatalf("normalize+list failed: %+v %v", all, err)
	}
	if err := s.DeleteActionListenerAddresseeByName(ctx, "GWACT"); err != nil {
		t.Fatal(err)
	}
	all, _ = s.ListActionListenerAddressees(ctx)
	if len(all) != 0 {
		t.Fatalf("expected empty after delete, got %v", all)
	}
}

func TestListenerAddresseeRejectsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateActionListenerAddressee(ctx, "   "); err == nil {
		t.Fatal("expected error for empty addressee")
	}
}

func TestListenerAddresseeRejectsTooLong(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// 10 characters > 9-char ax.25 addressee limit.
	if err := s.CreateActionListenerAddressee(ctx, "ABCDEFGHIJ"); err == nil {
		t.Fatal("expected error for >9-char addressee")
	}
}

func TestListenerAddresseeUniqueAfterNormalization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateActionListenerAddressee(ctx, "GWACT"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateActionListenerAddressee(ctx, "gwact"); err == nil {
		t.Fatal("expected unique-violation after uppercase normalization")
	}
}
