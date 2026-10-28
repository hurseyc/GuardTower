package auth

import "testing"

func TestHashAndVerify(t *testing.T) {
	h, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := VerifyPassword(h, "correct-horse-battery"); err != nil || !ok {
		t.Fatalf("verify good: %v %v", ok, err)
	}
	if ok, _ := VerifyPassword(h, "wrong"); ok {
		t.Fatal("verify bad password succeeded")
	}
	h2, _ := HashPassword("correct-horse-battery")
	if h == h2 {
		t.Fatal("hashes must be salted")
	}
	if _, err := VerifyPassword("$2a$10$bcrypt", "x"); err == nil {
		t.Fatal("expected error for unsupported hash")
	}
}

func TestLimiter(t *testing.T) {
	l := newLoginLimiter()
	for i := 0; i < limiterMax; i++ {
		if !l.allow("k") {
			t.Fatalf("blocked too early at %d", i)
		}
		l.fail("k")
	}
	if l.allow("k") {
		t.Fatal("expected block after max failures")
	}
	l.reset("k")
	if !l.allow("k") {
		t.Fatal("expected allow after reset")
	}
}
