import { defineConfig } from "@playwright/test";
import shared from "./playwright.config";

// Visual baselines need a fresh backend so earlier specs cannot change the
// rendered data.
export default defineConfig({ ...shared, testMatch: "visual.spec.ts", testIgnore: undefined });
