import { defineConfig } from '@playwright/test';
import base from './playwright.config';

// Activity ranks executions across the installation. Use a fresh backend so
// executions from other journeys cannot fill the ranking before this test.
export default defineConfig({ ...base, testMatch: 'work-activity.spec.ts', testIgnore: [] });
