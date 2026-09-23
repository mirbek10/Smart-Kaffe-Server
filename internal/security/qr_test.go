package security

import "testing"

func TestEncryptedQR(t *testing.T) {
	const url = "https://cafe.example/menu/table/secret-capability"
	first, e := EncryptQR(url, "table-1", "key")
	if e != nil {
		t.Fatal(e)
	}
	second, e := EncryptQR(url, "table-1", "key")
	if e != nil || first == second {
		t.Fatal("nonce must be random")
	}
	actual, e := DecryptQR(first, "table-1", "key")
	if e != nil || actual != url {
		t.Fatal("QR round trip failed")
	}
	for _, args := range [][3]string{{first, "table-2", "key"}, {first, "table-1", "wrong-key"}, {"invalid", "table-1", "key"}} {
		if _, e := DecryptQR(args[0], args[1], args[2]); e == nil {
			t.Fatal("invalid encrypted QR accepted")
		}
	}
}
