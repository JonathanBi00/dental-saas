import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// Routes are code-split, so the first render of a screen waits on a cold dynamic import.
configure({ asyncUtilTimeout: 5000 });

// jsdom doesn't implement scrolling; the router calls it on navigation.
window.scrollTo = () => {};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});
