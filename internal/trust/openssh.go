package trust

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
)

const openSSHEd25519 = "ssh-ed25519"

// ParseOpenSSHPublicKey converts a one-line OpenSSH Ed25519 public key to the
// raw key representation used by trust metadata. Comments are ignored.
func ParseOpenSSHPublicKey(value string) (ed25519.PublicKey, error) {
	fields := strings.Fields(value)
	if len(fields) < 2 || fields[0] != openSSHEd25519 {
		return nil, fmt.Errorf("public key is not an OpenSSH Ed25519 key")
	}
	wire, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return nil, fmt.Errorf("invalid OpenSSH public key encoding")
	}
	algorithm, rest, err := readSSHString(wire)
	if err != nil || string(algorithm) != openSSHEd25519 {
		return nil, fmt.Errorf("invalid OpenSSH Ed25519 key algorithm")
	}
	public, rest, err := readSSHString(rest)
	if err != nil || len(rest) != 0 || len(public) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid OpenSSH Ed25519 public key")
	}
	return ed25519.PublicKey(append([]byte(nil), public...)), nil
}

// EncodeOpenSSHPublicKey returns an OpenSSH public-key line without a comment.
func EncodeOpenSSHPublicKey(public ed25519.PublicKey) (string, error) {
	if len(public) != ed25519.PublicKeySize {
		return "", fmt.Errorf("invalid Ed25519 public key")
	}
	wire := appendSSHString(nil, []byte(openSSHEd25519))
	wire = appendSSHString(wire, public)
	return openSSHEd25519 + " " + base64.StdEncoding.EncodeToString(wire), nil
}

func readSSHString(value []byte) ([]byte, []byte, error) {
	if len(value) < 4 {
		return nil, nil, fmt.Errorf("truncated SSH string")
	}
	size := uint64(binary.BigEndian.Uint32(value[:4]))
	if size > uint64(len(value)-4) {
		return nil, nil, fmt.Errorf("truncated SSH string")
	}
	end := 4 + int(size)
	return value[4:end], value[end:], nil
}

func appendSSHString(destination, value []byte) []byte {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	destination = append(destination, length[:]...)
	return append(destination, value...)
}
