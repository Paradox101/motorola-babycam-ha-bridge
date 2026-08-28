// Export defined strings and incoming references for protocol reconstruction.
// @category VM65

import ghidra.app.script.GhidraScript;
import ghidra.program.model.address.Address;
import ghidra.program.model.data.StringDataInstance;
import ghidra.program.model.listing.Data;
import ghidra.program.model.listing.Listing;
import ghidra.program.model.symbol.Reference;
import java.io.File;
import java.io.PrintWriter;

public class ExportStringsAndRefs extends GhidraScript {
    @Override
    public void run() throws Exception {
        if (getScriptArgs().length != 1) {
            throw new IllegalArgumentException("usage: ExportStringsAndRefs.java <output-file>");
        }
        Listing listing = currentProgram.getListing();
        try (PrintWriter out = new PrintWriter(new File(getScriptArgs()[0]), "UTF-8")) {
            for (Data data : listing.getDefinedData(true)) {
                if (!data.hasStringValue()) continue;
                StringDataInstance value = StringDataInstance.getStringDataInstance(data);
                String text = value.getStringValue();
                if (text == null) continue;
                Address address = data.getAddress();
                out.printf("%s\t%s", address, escape(text));
                Reference[] refs = getReferencesTo(address);
                if (refs.length > 0) {
                    out.print("\tREFS=");
                    for (int i = 0; i < refs.length; i++) {
                        if (i > 0) out.print(",");
                        out.print(refs[i].getFromAddress());
                    }
                }
                out.println();
            }
        }
    }

    private String escape(String value) {
        return value.replace("\\", "\\\\")
                    .replace("\r", "\\r")
                    .replace("\n", "\\n")
                    .replace("\t", "\\t");
    }
}
