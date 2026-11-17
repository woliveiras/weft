import { defineConfig } from "@playwright/test";
import shared from "./playwright.config";

export default defineConfig({ ...shared, testMatch: "instructions.spec.ts" });
