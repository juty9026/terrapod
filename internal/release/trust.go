package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

// TrustProof preserves the exact signed bytes that authorized trusted-key
// additions. The byte slices are encoded as base64 by encoding/json.
type TrustProof struct {
	Manifest  []byte `json:"manifest"`
	Signature []byte `json:"signature"`
}

// PersistedTrust contains stored proofs and facts derived by verifying them
// from the compiled roots. Derived facts are never serialized.
type PersistedTrust struct {
	Proofs      []TrustProof                 `json:"proofs"`
	Keys        map[string]ed25519.PublicKey `json:"-"`
	Provenance  map[string]string            `json:"-"`
	ProofDigest string                       `json:"-"`
}

func VerifyProofChain(compiled map[string]ed25519.PublicKey, proofs []TrustProof) (PersistedTrust, error) {
	trusted, _, err := (Verifier{CompiledKeys: compiled}).effectiveTrust()
	if err != nil {
		return PersistedTrust{}, err
	}
	provenance := make(map[string]string)
	seen := make(map[string]struct{}, len(proofs))
	for index, proof := range proofs {
		if len(proof.Manifest) == 0 || len(proof.Manifest) > MaxManifestSize || len(proof.Signature) == 0 || len(proof.Signature) > MaxManifestSize {
			return PersistedTrust{}, fmt.Errorf("trust proof %d has invalid size", index)
		}
		digest := trustProofDigest(proof)
		if _, duplicate := seen[digest]; duplicate {
			return PersistedTrust{}, fmt.Errorf("duplicate trust proof %d", index)
		}
		seen[digest] = struct{}{}
		verifier := Verifier{CompiledKeys: trusted}
		manifest, err := verifier.VerifyManifest(proof.Manifest, proof.Signature)
		if err != nil {
			return PersistedTrust{}, fmt.Errorf("verify trust proof %d: %w", index, err)
		}
		if len(manifest.TrustedKeys) == 0 {
			return PersistedTrust{}, fmt.Errorf("trust proof %d authorizes no keys", index)
		}
		trusted, err = verifier.TrustAfter(manifest)
		if err != nil {
			return PersistedTrust{}, fmt.Errorf("derive trust proof %d: %w", index, err)
		}
		manifestDigest, _ := manifest.Digest()
		for _, addition := range manifest.TrustedKeys {
			provenance[addition.ID] = manifestDigest
		}
	}
	derived := make(map[string]ed25519.PublicKey, len(trusted))
	for id, key := range trusted {
		if _, root := compiled[id]; !root {
			derived[id] = append(ed25519.PublicKey(nil), key...)
		}
	}
	return PersistedTrust{Proofs: cloneProofs(proofs), Keys: derived, Provenance: provenance, ProofDigest: trustProofChainDigest(proofs)}, nil
}

func BuildPersistedTrust(compiled map[string]ed25519.PublicKey, prior []TrustProof, current VerifiedRelease) (PersistedTrust, error) {
	verified, err := VerifyProofChain(compiled, prior)
	if err != nil {
		return PersistedTrust{}, err
	}
	if err := current.verifySeal(); err != nil {
		return PersistedTrust{}, err
	}
	if len(current.Manifest.TrustedKeys) == 0 {
		return verified, nil
	}
	proof := TrustProof{Manifest: append([]byte(nil), current.manifestData...), Signature: append([]byte(nil), current.signatureData...)}
	digest := trustProofDigest(proof)
	for _, existing := range prior {
		if trustProofDigest(existing) != digest {
			continue
		}
		if !bytes.Equal(existing.Manifest, proof.Manifest) || !bytes.Equal(existing.Signature, proof.Signature) {
			return PersistedTrust{}, errors.New("trust proof digest collision")
		}
		return verified, nil
	}
	return VerifyProofChain(compiled, append(cloneProofs(prior), proof))
}

func trustProofDigest(proof TrustProof) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("terrapod-trust-proof-v1\x00"))
	_ = binary.Write(hash, binary.BigEndian, uint64(len(proof.Manifest)))
	_, _ = hash.Write(proof.Manifest)
	_ = binary.Write(hash, binary.BigEndian, uint64(len(proof.Signature)))
	_, _ = hash.Write(proof.Signature)
	return hex.EncodeToString(hash.Sum(nil))
}

func trustProofChainDigest(proofs []TrustProof) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("terrapod-trust-chain-v1\x00"))
	for _, proof := range proofs {
		digest, _ := hex.DecodeString(trustProofDigest(proof))
		_, _ = hash.Write(digest)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cloneProofs(proofs []TrustProof) []TrustProof {
	cloned := make([]TrustProof, len(proofs))
	for i, proof := range proofs {
		cloned[i] = TrustProof{Manifest: append([]byte(nil), proof.Manifest...), Signature: append([]byte(nil), proof.Signature...)}
	}
	return cloned
}
