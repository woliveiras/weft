import {defineConfig} from '@playwright/test';
import base from './playwright.config';

// Provider CRUD reloads the model registry. Use a fresh backend so the injected
// deterministic models of the main journey suite cannot be replaced by this test.
export default defineConfig({...base,testDir:'./e2e-provider-management',testIgnore:[]});
