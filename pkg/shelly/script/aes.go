package script

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"

	"github.com/dop251/goja"
)

const aesDefaultMode = "CBC"

// installAES adds the AES global (AES.encrypt/AES.decrypt):
// https://shelly-api-docs.shelly.cloud/gen2/Scripts/APIs/AES
//
// The documented API takes no IV (initialization vector) parameter, so —
// matching the real device — encryption/decryption uses an all-zero IV.
// This mirrors the Gen3/4 scripting API as documented, not a general-purpose
// crypto primitive: don't reuse this construction where IV reuse matters.
func installAES(vm *goja.Runtime) {
	aesObj := vm.NewObject()
	aesObj.Set("encrypt", func(call goja.FunctionCall) goja.Value {
		return aesTransform(vm, call, true)
	})
	aesObj.Set("decrypt", func(call goja.FunctionCall) goja.Value {
		return aesTransform(vm, call, false)
	})
	vm.Set("AES", aesObj)
}

func aesTransform(vm *goja.Runtime, call goja.FunctionCall, encrypt bool) goja.Value {
	data, err := exportArrayBufferBytes(call.Argument(0))
	if err != nil {
		panic(vm.ToValue(fmt.Sprintf("AES: data argument: %v", err)))
	}
	key, err := exportArrayBufferBytes(call.Argument(1))
	if err != nil {
		panic(vm.ToValue(fmt.Sprintf("AES: key argument: %v", err)))
	}
	mode := aesDefaultMode
	if len(call.Arguments) > 2 && !goja.IsUndefined(call.Argument(2)) {
		mode = call.Argument(2).String()
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		panic(vm.ToValue(fmt.Sprintf("AES: %v", err)))
	}

	out, err := aesCrypt(block, mode, data, encrypt)
	if err != nil {
		panic(vm.ToValue(fmt.Sprintf("AES: %v", err)))
	}
	return vm.ToValue(vm.NewArrayBuffer(out))
}

// aesCrypt runs one AES transform. iv is always zero (see installAES doc).
func aesCrypt(block cipher.Block, mode string, data []byte, encrypt bool) ([]byte, error) {
	iv := make([]byte, aes.BlockSize)

	switch mode {
	case "CBC":
		if len(data)%aes.BlockSize != 0 {
			return nil, fmt.Errorf("CBC: input length %d is not a multiple of the block size (%d)", len(data), aes.BlockSize)
		}
		out := make([]byte, len(data))
		if encrypt {
			cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, data)
		} else {
			cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)
		}
		return out, nil
	case "CFB":
		out := make([]byte, len(data))
		if encrypt {
			cipher.NewCFBEncrypter(block, iv).XORKeyStream(out, data) //nolint:staticcheck // CFB is part of the documented Shelly API surface
		} else {
			cipher.NewCFBDecrypter(block, iv).XORKeyStream(out, data) //nolint:staticcheck
		}
		return out, nil
	case "OFB":
		out := make([]byte, len(data))
		cipher.NewOFB(block, iv).XORKeyStream(out, data) //nolint:staticcheck // OFB is part of the documented Shelly API surface
		return out, nil
	case "CTR":
		out := make([]byte, len(data))
		cipher.NewCTR(block, iv).XORKeyStream(out, data)
		return out, nil
	case "ECB":
		// crypto/cipher deliberately omits ECB (insecure for general use),
		// so each block is run through the raw cipher.Block directly. ECB
		// is part of the documented Shelly API surface, so it's emulated
		// here despite that.
		if len(data)%aes.BlockSize != 0 {
			return nil, fmt.Errorf("ECB: input length %d is not a multiple of the block size (%d)", len(data), aes.BlockSize)
		}
		out := make([]byte, len(data))
		for i := 0; i < len(data); i += aes.BlockSize {
			if encrypt {
				block.Encrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
			} else {
				block.Decrypt(out[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported mode %q (want CBC, CFB, CTR, OFB, or ECB)", mode)
	}
}

func exportArrayBufferBytes(v goja.Value) ([]byte, error) {
	exported := v.Export()
	ab, ok := exported.(goja.ArrayBuffer)
	if !ok {
		return nil, fmt.Errorf("expected ArrayBuffer, got %T", exported)
	}
	return ab.Bytes(), nil
}
