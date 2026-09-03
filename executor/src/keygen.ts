// Generates a THROWAWAY testnet-only key and writes .env if absent.
// Prints the address only — never the key.
import { generatePrivateKey, privateKeyToAccount } from "viem/accounts";
import { existsSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const ENV = fileURLToPath(new URL("../.env", import.meta.url));

if (existsSync(ENV)) {
  console.log(".env already exists — leaving it untouched.");
  process.exit(0);
}
const pk = generatePrivateKey();
const account = privateKeyToAccount(pk);
writeFileSync(
  ENV,
  [
    "# Firstbid — Shannon testnet ONLY. Throwaway key. Never fund with real value.",
    `FIRSTBID_PRIVATE_KEY=${pk}`,
    `FIRSTBID_ADDRESS=${account.address}`,
    "FIRSTBID_INDEXER_URL=https://dev.smk.somnia.host/v1/graphql",
    "FIRSTBID_WS_RPC_URL=wss://api.infra.testnet.somnia.network/ws",
    "FIRSTBID_PORT=8787",
    "",
  ].join("\n"),
  { mode: 0o600 },
);
console.log("Wrote .env (mode 600).");
console.log("Fund this address with STT gas:", account.address);
