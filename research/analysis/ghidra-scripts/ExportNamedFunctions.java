// Export decompiler output and direct references for selected libdevconn symbols.
// @category VM65

import java.io.File;
import java.io.PrintWriter;
import java.util.Arrays;
import java.util.Iterator;

import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.app.script.GhidraScript;
import ghidra.program.model.listing.Function;
import ghidra.program.model.symbol.Reference;
import ghidra.util.task.ConsoleTaskMonitor;

public class ExportNamedFunctions extends GhidraScript {
    private static final String[] DEFAULT_NAMES = {
        "magicp2p_connect_device_v1", "magicp2p_connect_device_v3",
        "magicp2p_connect_device_generic", "magicp2p_aio_app_bridge_create",
        "magicp2p_aio_data_send", "magicp2p_aio_data_recv",
        "magicp2p_aio_data_process", "token_crypto_init_buffer",
        "token_crypto_encode", "token_crypto_decode", "token_crypto_send",
        "token_crypto_receive", "crypto_auth_hmacsha256",
        "magic_nwk_connect_send", "magic_nwk_socket_connect",
        "relay_thread", "relay_header", "FUN_000162dc", "FUN_000171bc", "FUN_000172a4",
        "FUN_00017cf0", "FUN_00018144", "FUN_000195cc",
        "magic_crypt_hash", "magic_crypt_encode", "magic_crypt_decode",
        "FUN_00021530", "FUN_00020f4c", "FUN_00021898", "FUN_000218b8",
        "FUN_00021918", "FUN_00021628", "generate_sid_v1"
    };

    @Override
    public void run() throws Exception {
        String[] args = getScriptArgs();
        if (args.length < 1) {
            throw new IllegalArgumentException("output path required");
        }
        String[] names = args.length > 1 ? Arrays.copyOfRange(args, 1, args.length) : DEFAULT_NAMES;
        DecompInterface decompiler = new DecompInterface();
        decompiler.openProgram(currentProgram);
        try (PrintWriter out = new PrintWriter(new File(args[0]), "UTF-8")) {
            for (String name : names) {
                Iterator<Function> functions = currentProgram.getFunctionManager().getFunctions(true);
                Function found = null;
                while (functions.hasNext()) {
                    Function candidate = functions.next();
                    if (candidate.getName().equals(name)) { found = candidate; break; }
                }
                out.println("\n================================================================================");
                out.println("FUNCTION " + name);
                if (found == null) { out.println("NOT FOUND"); continue; }
                out.println("ENTRY " + found.getEntryPoint() + " SIGNATURE " + found.getSignature());
                out.println("CALLERS:");
                for (Reference ref : currentProgram.getReferenceManager().getReferencesTo(found.getEntryPoint())) {
                    Function caller = currentProgram.getFunctionManager().getFunctionContaining(ref.getFromAddress());
                    out.println("  " + ref.getFromAddress() + " " + (caller == null ? "<none>" : caller.getName()));
                }
                DecompileResults result = decompiler.decompileFunction(found, 120, new ConsoleTaskMonitor());
                if (!result.decompileCompleted()) {
                    out.println("DECOMPILE FAILED: " + result.getErrorMessage());
                } else {
                    out.println(result.getDecompiledFunction().getC());
                }
            }
        } finally {
            decompiler.dispose();
        }
    }
}
