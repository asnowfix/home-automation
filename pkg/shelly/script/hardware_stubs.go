package script

import (
	"fmt"

	"github.com/dop251/goja"
)

// installHardwareStubs registers the Shelly Gen2 scripting globals that the
// emulator cannot meaningfully fake: HTTPServer, BLE, BTHome, and UART all
// depend on physical radios or pins the daemon/test host doesn't have. They
// are still registered — so scripts referencing them get a clear, catchable
// "not implemented by the emulator" exception — instead of being left
// undefined, which would surface as an unrelated ReferenceError.
func installHardwareStubs(vm *goja.Runtime) {
	httpServerObj := vm.NewObject()
	httpServerObj.Set("registerEndpoint", notImplemented(vm, "HTTPServer", "registerEndpoint"))
	vm.Set("HTTPServer", httpServerObj)

	bleObj := vm.NewObject()
	bleObj.Set("advertiseOnce", notImplemented(vm, "BLE", "advertiseOnce"))

	scannerObj := vm.NewObject()
	for _, method := range []string{"subscribe", "start", "stop", "isRunning", "getScanOptions"} {
		scannerObj.Set(method, notImplemented(vm, "BLE.Scanner", method))
	}
	bleObj.Set("Scanner", scannerObj)

	gapObj := vm.NewObject()
	for _, method := range []string{"parseName", "parseManufacturerData", "parseServiceData", "hasService"} {
		gapObj.Set(method, notImplemented(vm, "BLE.GAP", method))
	}
	bleObj.Set("GAP", gapObj)

	advBuilderObj := vm.NewObject()
	for _, method := range []string{"addName", "addShellyManufacturerData", "addServiceData", "addBTHomeServiceData", "build", "reset"} {
		advBuilderObj.Set(method, notImplemented(vm, "BLE.AdvBuilder", method))
	}
	bleObj.Set("AdvBuilder", advBuilderObj)

	vm.Set("BLE", bleObj)

	btHomeObj := vm.NewObject()
	btHomeObj.Set("parseData", notImplemented(vm, "BTHome", "parseData"))

	dataBuilderObj := vm.NewObject()
	for _, method := range []string{"addObject", "setTriggerBased", "build", "buildEncrypted", "reset"} {
		dataBuilderObj.Set(method, notImplemented(vm, "BTHome.DataBuilder", method))
	}
	btHomeObj.Set("DataBuilder", dataBuilderObj)

	vm.Set("BTHome", btHomeObj)

	uartObj := vm.NewObject()
	uartObj.Set("get", notImplemented(vm, "UART", "get"))
	vm.Set("UART", uartObj)
}

// notImplemented returns a script-callable function that immediately throws
// a catchable JS exception identifying the unsupported global/method.
func notImplemented(vm *goja.Runtime, global, method string) func(call goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		panic(vm.ToValue(fmt.Sprintf("%s.%s is not implemented by the script emulator (hardware-only Shelly API)", global, method)))
	}
}
