package hash

import "testing"

func TestArgon2HashVerifyRoundTrip(t *testing.T) {
	hasher := &Argon2Hasher{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}
	encoded, err := hasher.Hash("correct horse 1")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := hasher.Verify("correct horse 1", encoded)
	if err != nil || !ok {
		t.Fatalf("verify = %v, %v", ok, err)
	}
	ok, err = hasher.Verify("wrong", encoded)
	if err != nil || ok {
		t.Fatalf("wrong password verify = %v, %v", ok, err)
	}
}

func TestArgon2VerifyRejectsTamperedPHC(t *testing.T) {
	if ok, err := NewArgon2Hasher().Verify("password1", "$argon2id$v=19$m=bad$bad$bad"); err == nil || ok {
		t.Fatalf("verify = %v, %v", ok, err)
	}
}

func TestArgon2ProductionDefaults(t *testing.T) {
	hasher := NewArgon2Hasher()
	if hasher.Memory != 64*1024 || hasher.Iterations != 3 || hasher.Parallelism != 2 || hasher.SaltLen != 16 || hasher.KeyLen != 32 {
		t.Fatalf("unexpected defaults: %+v", hasher)
	}
}
