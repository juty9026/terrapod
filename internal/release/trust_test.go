package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"reflect"
	"testing"
)

func TestVerifyProofChainRequiresCompiledRootedOrderedProofs(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	next := ed25519.NewKeyFromSeed([]byte("abcdef0123456789abcdef0123456789"))
	last := ed25519.NewKeyFromSeed([]byte("fedcba9876543210fedcba9876543210"))
	first := trustProof(t, "root", root, "next", next.Public().(ed25519.PublicKey), "1.2.3")
	second := trustProof(t, "next", next, "last", last.Public().(ed25519.PublicKey), "1.2.4")
	compiled := map[string]ed25519.PublicKey{"root": root.Public().(ed25519.PublicKey)}

	got, err := VerifyProofChain(compiled, []TrustProof{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Keys, map[string]ed25519.PublicKey{"next": next.Public().(ed25519.PublicKey), "last": last.Public().(ed25519.PublicKey)}) {
		t.Fatalf("derived keys=%v", got.Keys)
	}
	if _, err := VerifyProofChain(compiled, []TrustProof{second, first}); err == nil {
		t.Fatal("unordered proof chain accepted")
	}
	if _, err := VerifyProofChain(compiled, []TrustProof{second}); err == nil {
		t.Fatal("unrooted proof accepted")
	}
	if _, err := VerifyProofChain(compiled, []TrustProof{first, first}); err == nil {
		t.Fatal("duplicate proof accepted")
	}
	forged := first
	forged.Manifest = append([]byte(nil), forged.Manifest...)
	forged.Manifest[len(forged.Manifest)-2] ^= 1
	if _, err := VerifyProofChain(compiled, []TrustProof{forged}); err == nil {
		t.Fatal("forged proof accepted")
	}
	wrongSigner := trustProof(t, "root", next, "last", last.Public().(ed25519.PublicKey), "1.2.5")
	if _, err := VerifyProofChain(compiled, []TrustProof{wrongSigner}); err == nil {
		t.Fatal("proof signed by the wrong key accepted")
	}
	cycleA := trustProof(t, "last", last, "next", next.Public().(ed25519.PublicKey), "1.2.5")
	cycleB := trustProof(t, "next", next, "last", last.Public().(ed25519.PublicKey), "1.2.6")
	if _, err := VerifyProofChain(compiled, []TrustProof{cycleA, cycleB}); err == nil {
		t.Fatal("unrooted cyclic proof chain accepted")
	}
	compiledAlias := trustProof(t, "root", root, "root-alias", root.Public().(ed25519.PublicKey), "1.2.7")
	if _, err := VerifyProofChain(compiled, []TrustProof{compiledAlias}); err == nil {
		t.Fatal("compiled root material alias accepted")
	}
	derivedAlias := trustProof(t, "next", next, "next-alias", next.Public().(ed25519.PublicKey), "1.2.8")
	if _, err := VerifyProofChain(compiled, []TrustProof{first, derivedAlias}); err == nil {
		t.Fatal("derived key material alias accepted")
	}
	sameManifest := validManifest(t)
	sameManifest.Version = "1.2.9"
	encodedNext := base64.StdEncoding.EncodeToString(next.Public().(ed25519.PublicKey))
	sameManifest.TrustedKeys = []TrustedKey{{ID: "next", PublicKey: encodedNext}, {ID: "next-alias", PublicKey: encodedNext}}
	sameData := encodeManifest(t, sameManifest)
	if _, err := VerifyProofChain(compiled, []TrustProof{{Manifest: sameData, Signature: signManifest(t, "root", root, sameData)}}); err == nil {
		t.Fatal("same-manifest key material aliases accepted")
	}
}

func TestBuildPersistedTrustIsExactProofIdempotent(t *testing.T) {
	root := ed25519.NewKeyFromSeed(testSeed)
	next := ed25519.NewKeyFromSeed([]byte("abcdef0123456789abcdef0123456789"))
	proof := trustProof(t, "root", root, "next", next.Public().(ed25519.PublicKey), "1.2.3")
	verifier := testVerifier(root.Public().(ed25519.PublicKey))
	manifest, err := verifier.VerifyManifest(proof.Manifest, proof.Signature)
	if err != nil {
		t.Fatal(err)
	}
	current := VerifiedRelease{Manifest: manifest, manifestData: proof.Manifest, signatureData: proof.Signature}
	if err := current.sealManifest(); err != nil {
		t.Fatal(err)
	}
	compiled := map[string]ed25519.PublicKey{"root": root.Public().(ed25519.PublicKey)}
	first, err := BuildPersistedTrust(compiled, nil, current)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPersistedTrust(compiled, first.Proofs, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Proofs) != 1 || first.ProofDigest != second.ProofDigest {
		t.Fatalf("same proof duplicated: first=%#v second=%#v", first, second)
	}
}

func trustProof(t *testing.T, signerID string, signer ed25519.PrivateKey, additionID string, addition ed25519.PublicKey, version string) TrustProof {
	t.Helper()
	manifest := validManifest(t)
	manifest.Version = version
	manifest.TrustedKeys = []TrustedKey{{ID: additionID, PublicKey: base64.StdEncoding.EncodeToString(addition)}}
	data := encodeManifest(t, manifest)
	return TrustProof{Manifest: data, Signature: signManifest(t, signerID, signer, data)}
}
