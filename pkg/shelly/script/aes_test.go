package script

import (
	"encoding/hex"
	"testing"

	"github.com/dop251/goja"
)

func newAESTestVM(t *testing.T) *goja.Runtime {
	t.Helper()
	vm := goja.New()
	installAES(vm)
	return vm
}

// TestAESECBKnownVector checks against the FIPS-197 AES-128 known-answer
// test vector. ECB needs no IV, so it's the one mode that lets us verify the
// underlying cipher against a published vector rather than only round-trips.
func TestAESECBKnownVector(t *testing.T) {
	vm := newAESTestVM(t)
	vm.Set("hexToBuf", func(call goja.FunctionCall) goja.Value {
		b, err := hex.DecodeString(call.Argument(0).String())
		if err != nil {
			panic(vm.ToValue(err.Error()))
		}
		return vm.ToValue(vm.NewArrayBuffer(b))
	})
	vm.Set("bufToHex", func(call goja.FunctionCall) goja.Value {
		ab, _ := call.Argument(0).Export().(goja.ArrayBuffer)
		return vm.ToValue(hex.EncodeToString(ab.Bytes()))
	})

	result, err := vm.RunString(`
		var key = hexToBuf("000102030405060708090a0b0c0d0e0f");
		var plain = hexToBuf("00112233445566778899aabbccddeeff");
		bufToHex(AES.encrypt(plain, key, "ECB"));
	`)
	if err != nil {
		t.Fatalf("RunString: %v", err)
	}
	want := "69c4e0d86a7b0430d8cdb78070b4c55a"
	if got := result.String(); got != want {
		t.Errorf("AES-128-ECB(known vector) = %s, want %s", got, want)
	}
}

func TestAESRoundTripPerMode(t *testing.T) {
	modes := []string{"CBC", "CFB", "CTR", "OFB", "ECB"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			vm := newAESTestVM(t)
			vm.Set("mode", mode)
			result, err := vm.RunString(`
				var key = new Uint8Array(16);
				for (var i = 0; i < 16; i++) key[i] = i;
				var plain = new Uint8Array(32); // 2 blocks: also exercises block modes
				for (var i = 0; i < 32; i++) plain[i] = 100 + i;
				var cipherText = AES.encrypt(plain.buffer, key.buffer, mode);
				var decrypted = new Uint8Array(AES.decrypt(cipherText, key.buffer, mode));
				var ok = decrypted.length === plain.length;
				if (ok) {
					for (var i = 0; i < plain.length; i++) {
						if (decrypted[i] !== plain[i]) { ok = false; break; }
					}
				}
				ok;
			`)
			if err != nil {
				t.Fatalf("RunString: %v", err)
			}
			if !result.ToBoolean() {
				t.Errorf("%s: decrypt(encrypt(plain)) != plain", mode)
			}
		})
	}
}

func TestAESCBCRejectsUnalignedLength(t *testing.T) {
	vm := newAESTestVM(t)
	result, err := vm.RunString(`
		(function() {
			try {
				var key = new Uint8Array(16);
				var plain = new Uint8Array(5); // not a multiple of 16
				AES.encrypt(plain.buffer, key.buffer, "CBC");
				return "no error thrown";
			} catch (e) {
				return String(e);
			}
		})();
	`)
	if err != nil {
		t.Fatalf("RunString returned a Go error (exception escaped the VM): %v", err)
	}
	if result.String() == "no error thrown" {
		t.Error("CBC accepted a non-block-aligned length, want a thrown exception")
	}
}

func TestAESUnsupportedModeThrows(t *testing.T) {
	vm := newAESTestVM(t)
	result, err := vm.RunString(`
		(function() {
			try {
				var key = new Uint8Array(16);
				var plain = new Uint8Array(16);
				AES.encrypt(plain.buffer, key.buffer, "GCM");
				return "no error thrown";
			} catch (e) {
				return String(e);
			}
		})();
	`)
	if err != nil {
		t.Fatalf("RunString returned a Go error (exception escaped the VM): %v", err)
	}
	if result.String() == "no error thrown" {
		t.Error("unsupported mode was accepted, want a thrown exception")
	}
}
