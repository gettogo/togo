package appinventory

import (
	"encoding/json"
	"fmt"
	"io"
)

// DecodeProof reads one JSON array of executed UI assertion records.
func DecodeProof(r io.Reader) ([]Proof, error) {
	d := json.NewDecoder(r)
	if err := delimiter(d, '['); err != nil {
		return nil, err
	}
	var out []Proof
	for d.More() {
		proof, err := decodeProofRecord(d)
		if err != nil {
			return nil, fmt.Errorf("proof record %d: %w", len(out)+1, err)
		}
		out = append(out, proof)
	}
	if err := delimiter(d, ']'); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("extra content after proof array")
	}
	return out, nil
}

func decodeProofRecord(d *json.Decoder) (Proof, error) {
	var proof Proof
	if err := delimiter(d, '{'); err != nil {
		return proof, err
	}
	fields := map[string]*string{
		"key": &proof.Key, "revision": &proof.Revision,
		"tier": &proof.Tier, "result": &proof.Result,
	}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return Proof{}, err
		}
		key, ok := token.(string)
		if !ok || fields[key] == nil {
			return Proof{}, fmt.Errorf("unexpected or duplicate proof field %q", token)
		}
		value := fields[key]
		if err := d.Decode(value); err != nil {
			return Proof{}, fmt.Errorf("proof field %s: %w", key, err)
		}
		if *value == "" {
			return Proof{}, fmt.Errorf("proof field %s must be nonempty", key)
		}
		delete(fields, key)
	}
	if err := delimiter(d, '}'); err != nil {
		return Proof{}, err
	}
	if len(fields) != 0 {
		return Proof{}, fmt.Errorf("proof requires key, revision, tier and result")
	}
	return proof, nil
}

func delimiter(d *json.Decoder, want json.Delim) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token != want {
		return fmt.Errorf("expected %q in proof artifact", want)
	}
	return nil
}
