import {defineConfig} from '@playwright/test';
import base from './playwright.config';
export default defineConfig({...base,testIgnore:[],testMatch:'mcp-cimd.spec.ts',use:{...base.use,baseURL:'https://weft.fixture.example:18091',launchOptions:{args:['--host-resolver-rules=MAP weft.fixture.example 127.0.0.1']}},webServer:{...base.webServer,command:'pnpm run build && cd ../server && go build -tags e2e -o bin/weft-e2e ./cmd/weft-e2e && WEFT_E2E_CIMD=1 WEFT_E2E_PORT=18090 exec bin/weft-e2e',url:'http://127.0.0.1:18090/healthz'}});
