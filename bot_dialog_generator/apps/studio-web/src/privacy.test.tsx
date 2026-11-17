import { render, screen, fireEvent, waitFor, within, act, cleanup } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import cookiesDoc from "../../../docs/legal/COOKIES_AND_STORAGE.md?raw";
import { App, Studio } from "./App";
import { Login } from "./Login";
import { Footer } from "./Footer";
import { Markdown } from "./Markdown";
import { LEGAL_DOCUMENTS, businessInfo, fillPlaceholders, legalDocumentForHash } from "./legal";
import { SESSION_KEY, TERMS_PENDING_KEY } from "./api";
import { CHOICES_MAX_AGE_MS, PRIVACY_CHOICES_KEY, loadChoices, saveChoices } from "./privacyChoices";
import { alice, botsByWorkspace, carol, fakeApi } from "./test/fixtures";

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
  window.location.hash = "";
});

afterEach(() => {
  // Unmount before restoring fetch, so no late effect reaches the real network.
  cleanup();
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const renderStudio = (api = fakeApi(alice), onTrack = vi.fn()) => {
  render(<Studio api={api} onSignOut={vi.fn()} onTrack={onTrack} />);
  return { api, onTrack };
};

describe("Legal documents", () => {
  it("serves every document in docs/legal at #/legal/<page>, without signing in", async () => {
    for (const doc of LEGAL_DOCUMENTS) {
      expect(doc.source.length, doc.file).toBeGreaterThan(200);
      expect(legalDocumentForHash(`#/legal/${doc.slug}`)).toBe(doc);
    }
    expect(legalDocumentForHash("#/legal/nope")).toBeNull();
    expect(legalDocumentForHash("#main-content")).toBeUndefined();

    window.location.hash = "#/legal/privacy";
    render(<App companySignIn={null} />);
    expect(await screen.findByRole("heading", { level: 1, name: "Privacy Policy" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Sign in" })).toBeNull();
    const nav = screen.getByRole("navigation", { name: "Legal documents" });
    expect(within(nav).getByRole("link", { name: "Privacy Policy" }).getAttribute("aria-current")).toBe("page");
  });

  it("follows the hash to another document, and says when a page does not exist", async () => {
    window.location.hash = "#/legal/terms";
    render(<App companySignIn={null} />);
    await screen.findByRole("heading", { level: 1, name: "Terms of Use" });
    act(() => {
      window.location.hash = "#/legal/does-not-exist";
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Page not found" })).toBeTruthy();
  });

  it("documents the refund position, the business contact, licenses and third parties", () => {
    const text = (slug: string) => LEGAL_DOCUMENTS.find((d) => d.slug === slug)!.source;
    expect(text("refunds")).toContain("There are no charges today");
    expect(text("business")).toMatch(/Tax ID[\s\S]*Data Protection Officer/);
    expect(text("licenses")).toContain("SIL Open Font License");
    expect(text("third-parties")).toContain("No trackers");
    expect(text("privacy")).toContain("row-level security");
    expect(text("privacy")).toContain("WEBCHAT_WORKSPACE_ID");
    expect(text("privacy")).toMatch(/train/);
    expect(text("terms")).toMatch(/AI can be wrong/);
  });

  it("renders Markdown as text: links between documents work and nothing runs as code", () => {
    const { container } = render(
      <Markdown
        source={[
          "<!-- reviewer note -->",
          "# Title",
          "See the [Terms](TERMS_OF_USE.md) and [a site](https://example.com) or [evil](javascript:alert(1)).",
          "<script>window.hacked = true</script> <img src=x onerror=alert(1)>",
          "",
          "| Key | Value |",
          "|---|---|",
          "| `bdg.session` | **token** |",
          "",
          "- one",
          "- [[TAX_ID]]",
        ].join("\n")}
      />,
    );
    expect(container.textContent).not.toContain("reviewer note");
    expect(container.querySelector("script, img")).toBeNull();
    expect(container.textContent).toContain("<script>window.hacked = true</script>");
    const links = [...container.querySelectorAll("a")].map((a) => [a.textContent, a.getAttribute("href"), a.getAttribute("rel")]);
    expect(links).toEqual([
      ["Terms", "#/legal/terms", null],
      ["a site (opens in a new tab)", "https://example.com", "noopener noreferrer"],
    ]);
    expect(container.querySelector("th")?.getAttribute("scope")).toBe("col");
    expect(container.querySelector("td code")?.textContent).toBe("bdg.session");
    expect(container.querySelector("mark.placeholder")?.textContent).toBe("[[TAX_ID]]");
  });

  it("fills in the configured business details and leaves the rest visibly unfinished", () => {
    const info = businessInfo({ VITE_LEGAL_ENTITY_NAME: "Example Bank S.A.", VITE_LEGAL_TAX_ID: "900.123.456-7" } as ImportMetaEnv);
    const filled = fillPlaceholders("[[COMPANY_LEGAL_NAME]] · [[TAX_ID]] · [[PRIVACY_EMAIL]]", info);
    expect(filled).toBe("Example Bank S.A. · 900.123.456-7 · [[PRIVACY_EMAIL]]");
  });
});

describe("Footer and business information", () => {
  it("shows the legal entity, tax ID, address, support email and DPO, and links every document", () => {
    const info = businessInfo({
      VITE_LEGAL_ENTITY_NAME: "Example Bank S.A.",
      VITE_LEGAL_TAX_ID: "900.123.456-7",
      VITE_LEGAL_ADDRESS: "Calle 1 # 2-3, Bogotá",
      VITE_LEGAL_SUPPORT_EMAIL: "support@example.com",
      VITE_LEGAL_DPO_CONTACT: "dpo@example.com",
    } as ImportMetaEnv);
    render(<Footer info={info} />);
    const footer = screen.getByRole("contentinfo");
    for (const text of ["Example Bank S.A.", "Tax ID 900.123.456-7", "Calle 1 # 2-3, Bogotá", "Data Protection Officer dpo@example.com"]) {
      expect(footer.textContent).toContain(text);
    }
    expect(within(footer).getByRole("link", { name: "support@example.com" }).getAttribute("href")).toBe("mailto:support@example.com");
    const legal = within(within(footer).getByRole("navigation", { name: "Legal" })).getAllByRole("link");
    expect(legal.map((a) => a.getAttribute("href"))).toEqual(LEGAL_DOCUMENTS.map((d) => `#/legal/${d.slug}`));
  });

  it("shows placeholders, never invented values, when the details are not configured", () => {
    render(<Footer info={businessInfo({} as ImportMetaEnv)} />);
    const marks = [...screen.getByRole("contentinfo").querySelectorAll("mark")].map((m) => m.textContent);
    expect(marks).toEqual(["[[COMPANY_LEGAL_NAME]]", "[[TAX_ID]]", "[[REGISTERED_ADDRESS]]", "[[SUPPORT_EMAIL]]", "[[DPO_CONTACT]]"]);
  });

  it("is on the sign-in page, with a skip link first", () => {
    render(<App companySignIn={null} />);
    expect(screen.getByRole("contentinfo")).toBeTruthy();
    const skip = screen.getByRole("link", { name: "Skip to main content" });
    expect(document.body.querySelector("a")).toBe(skip);
    fireEvent.click(skip);
    expect(document.activeElement?.id).toBe("main-content");
    expect(window.location.hash).toBe("");
  });
});

describe("Terms acceptance at sign-in", () => {
  const terms = () => screen.getByRole("checkbox", { name: /accept the Terms of Use/ }) as HTMLInputElement;

  it("starts unchecked, links both documents, and blocks sign-in until it is ticked", async () => {
    const signIn = vi.fn(async () => "token");
    render(<Login onSignedIn={vi.fn()} signIn={signIn} termsVersion="2026-11" />);
    expect(terms().checked).toBe(false);
    const label = terms().closest("label")!;
    expect([...label.querySelectorAll("a")].map((a) => a.getAttribute("href"))).toEqual(["#/legal/terms", "#/legal/privacy"]);

    fireEvent.click(screen.getByText("Alice"));
    expect(screen.getByRole("alert").textContent).toContain("Tick the box");
    expect(document.activeElement).toBe(terms());
    expect(signIn).not.toHaveBeenCalled();

    fireEvent.click(terms());
    expect(screen.queryByRole("alert")).toBeNull();
    fireEvent.click(screen.getByText("Alice"));
    await waitFor(() => expect(signIn).toHaveBeenCalled());
    expect(sessionStorage.getItem(TERMS_PENDING_KEY)).toBe("2026-11");
  });

  it("records the accepted version once the studio knows who signed in", async () => {
    sessionStorage.setItem(TERMS_PENDING_KEY, "2026-11");
    const { api } = renderStudio();
    await waitFor(() => expect(api.acceptTerms).toHaveBeenCalledWith("2026-11"));
    await waitFor(() => expect(sessionStorage.getItem(TERMS_PENDING_KEY)).toBeNull());
  });
});

describe("Privacy banner", () => {
  const analyticsUrl = "https://stats.example.com";
  let posted: Array<{ url: string; body: any }>;

  beforeEach(() => {
    posted = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        posted.push({ url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
        return new Response(null, { status: 202 });
      }),
    );
  });

  it("does not appear when the studio has nothing optional to ask about", () => {
    render(<App companySignIn={null} analyticsUrl="" />);
    expect(screen.queryByRole("region", { name: "Privacy choices" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Privacy choices" })).toBeNull();
  });

  it("offers Accept, Reject and Customize with equal weight, without blocking the page", () => {
    render(<App companySignIn={null} analyticsUrl={analyticsUrl} />);
    const banner = screen.getByRole("region", { name: "Privacy choices" });
    const buttons = within(banner).getAllByRole("button");
    expect(buttons.map((b) => b.textContent)).toEqual(["Accept all", "Reject non-essential", "Customize"]);
    expect(new Set(buttons.map((b) => b.className)).size).toBe(1);
    // The sign-in page is still usable behind it.
    expect(screen.getByRole("heading", { name: "Sign in" })).toBeTruthy();
    expect(banner.getAttribute("aria-modal")).toBeNull();
  });

  it("sends nothing before an answer, nor after 'Reject non-essential'", () => {
    render(<App companySignIn={null} analyticsUrl={analyticsUrl} />);
    expect(posted).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "Reject non-essential" }));
    expect(screen.queryByRole("region", { name: "Privacy choices" })).toBeNull();
    expect(loadChoices()).toMatchObject({ analytics: false });
    expect(posted).toEqual([]);
  });

  it("counts anonymous events only after 'Accept all'", async () => {
    render(<App companySignIn={null} analyticsUrl={analyticsUrl} />);
    fireEvent.click(screen.getByRole("button", { name: "Accept all" }));
    expect(loadChoices()).toMatchObject({ analytics: true });
    // Signing in shows the studio, whose first tab view is counted.
    fireEvent.click(screen.getByRole("checkbox", { name: /accept the Terms of Use/ }));
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        posted.push({ url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
        if (url.endsWith("/dev/token")) return Response.json({ access_token: "t" });
        if (url.endsWith("/me")) return Response.json(alice);
        if (url.includes("/bots") && !url.includes("/versions")) return Response.json(botsByWorkspace["ws-customer-service"]);
        return Response.json([]);
      }),
    );
    fireEvent.click(screen.getByText("Alice"));
    await waitFor(() => expect(posted.some((p) => p.url === `${analyticsUrl}/events`)).toBe(true));
    const event = posted.find((p) => p.url === `${analyticsUrl}/events`)!.body;
    expect(event).toEqual({ source: "studio", event: "tab_viewed", consent: { analytics: true, campaigns: false }, props: { tab: "designer" } });
    expect(JSON.stringify(event)).not.toContain("alice");
  });

  it("lets people customize, with optional boxes unchecked, in a dialog that keeps focus", () => {
    render(<App companySignIn={null} analyticsUrl={analyticsUrl} />);
    const customize = screen.getByRole("button", { name: "Customize" });
    customize.focus();
    fireEvent.click(customize);
    const dialog = screen.getByRole("dialog", { name: "Privacy choices" });
    expect(dialog.getAttribute("aria-modal")).toBe("true");
    const box = within(dialog).getByRole("checkbox") as HTMLInputElement;
    expect(box.checked).toBe(false);
    expect(document.activeElement).toBe(box);

    // Tab from the last control wraps to the first.
    const buttons = within(dialog).getAllByRole("button");
    buttons.at(-1)!.focus();
    fireEvent.keyDown(dialog, { key: "Tab" });
    expect(document.activeElement).toBe(box);
    fireEvent.keyDown(dialog, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(buttons.at(-1));

    fireEvent.click(box);
    fireEvent.click(within(dialog).getByRole("button", { name: "Save choices" }));
    expect(loadChoices()).toMatchObject({ analytics: true });
    expect(screen.queryByRole("dialog")).toBeNull();

    // It can be reopened from the footer, and Escape closes it.
    fireEvent.click(screen.getByRole("button", { name: "Privacy choices" }));
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("asks again after a year or when the categories change", () => {
    const decided = new Date("2026-01-01T00:00:00Z");
    saveChoices(true, decided);
    expect(loadChoices(new Date(decided.getTime() + 1000))).toMatchObject({ analytics: true });
    expect(loadChoices(new Date(decided.getTime() + CHOICES_MAX_AGE_MS + 1))).toBeNull();
    localStorage.setItem(PRIVACY_CHOICES_KEY, JSON.stringify({ version: 0, analytics: true, decided_at: decided.toISOString() }));
    expect(loadChoices(new Date(decided.getTime() + 1000))).toBeNull();
  });
});

describe("Privacy tab: the right to erasure", () => {
  it("lets anyone ask for their own data to be deleted, after confirming", async () => {
    const { api } = renderStudio(fakeApi(carol));
    fireEvent.click(await screen.findByRole("button", { name: "Privacy" }));
    const open = await screen.findByRole("button", { name: "Request deletion of my data" });
    open.focus();
    fireEvent.click(open);
    const dialog = screen.getByRole("alertdialog", { name: "Request deletion of your data?" });
    const [send, cancel] = within(dialog).getAllByRole("button");
    expect([send.textContent, cancel.textContent]).toEqual(["Send request", "Cancel"]);
    expect(send.className).toBe(cancel.className);

    fireEvent.click(cancel);
    expect(api.requestMyDeletion).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(open);

    fireEvent.click(open);
    fireEvent.change(within(screen.getByRole("alertdialog")).getByRole("textbox"), { target: { value: "Leaving the team" } });
    fireEvent.click(screen.getByRole("button", { name: "Send request" }));
    await waitFor(() => expect(api.requestMyDeletion).toHaveBeenCalledWith("Leaving the team"));
    await waitFor(() =>
      expect(screen.getAllByRole("status").some((el) => el.textContent?.includes("Waiting for a platform administrator"))).toBe(true),
    );
  });

  it("lets owners delete a chat user's data only with a note on how identity was checked", async () => {
    const { api } = renderStudio();
    fireEvent.click(await screen.findByRole("button", { name: "Privacy" }));
    const form = await screen.findByRole("form", { name: "Delete a person's data" });
    fireEvent.change(within(form).getByLabelText("Conversation ID"), { target: { value: "usr-4f9a1c7e2b" } });
    fireEvent.click(within(form).getByRole("button", { name: "Delete this person's data" }));
    expect(within(form).getByRole("alert").textContent).toContain("how you checked");
    expect(document.activeElement).toBe(within(form).getByLabelText(/How you checked/));
    expect(api.eraseSubject).not.toHaveBeenCalled();

    fireEvent.change(within(form).getByLabelText(/How you checked/), { target: { value: "Replied from the chat session" } });
    fireEvent.click(within(form).getByRole("button", { name: "Delete this person's data" }));
    const confirm = screen.getByRole("alertdialog", { name: "Delete this person's data?" });
    expect(confirm.textContent).toContain("cannot be undone");
    fireEvent.click(within(confirm).getByRole("button", { name: "Delete data" }));
    await waitFor(() =>
      expect(api.eraseSubject).toHaveBeenCalledWith("ws-customer-service", {
        subject_kind: "chat_user",
        subject_id: "usr-4f9a1c7e2b",
        verification: "Replied from the chat session",
      }),
    );
    expect(await screen.findByText(/Reference/)).toBeTruthy();
    expect(screen.getAllByText(/Consent records/).length).toBeGreaterThan(0);
  });

  it("does not offer other people's erasure to editors and analysts", async () => {
    renderStudio(fakeApi(carol));
    fireEvent.click(await screen.findByRole("button", { name: "Privacy" }));
    await screen.findByRole("button", { name: "Request deletion of my data" });
    expect(screen.queryByRole("form", { name: "Delete a person's data" })).toBeNull();
    expect(screen.getByText(/Only owners of Customer service/)).toBeTruthy();
  });
});

describe("Bots for children", () => {
  it("saves who a bot is for", async () => {
    const { api } = renderStudio();
    fireEvent.click(await screen.findByRole("button", { name: "Versions" }));
    fireEvent.change(await screen.findByLabelText("Audience"), { target: { value: "minors" } });
    fireEvent.change(screen.getByLabelText("Minimum age (0 for none)"), { target: { value: "13" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(api.updateBotSettings).toHaveBeenCalledWith("ws-customer-service", "retail-assistant", { audience: "minors", min_age: 13 }));
  });

  it("asks the owner to confirm a parental-consent process before publishing", async () => {
    const minorsBot = { ...botsByWorkspace["ws-customer-service"][0], audience: "minors" as const, min_age: 13 };
    const api = fakeApi(alice, { listBots: vi.fn(async () => [minorsBot]) });
    renderStudio(api);
    await screen.findByText("Check balance");
    fireEvent.click(screen.getByRole("button", { name: "Publish bot" }));
    const dialog = screen.getByRole("alertdialog", { name: "Publish a bot for children?" });
    const box = within(dialog).getByRole("checkbox") as HTMLInputElement;
    expect(box.checked).toBe(false);
    const publish = within(dialog).getByRole("button", { name: "Publish" }) as HTMLButtonElement;
    expect(publish.disabled).toBe(true);
    fireEvent.click(box);
    fireEvent.click(publish);
    await waitFor(() =>
      expect(api.publish).toHaveBeenCalledWith("ws-customer-service", "retail-assistant", "v18", { parentalConsentConfirmed: true }),
    );
  });
});

describe("Keyboard and screen readers", () => {
  it("keeps focus in the test chat, closes it with Escape and returns focus to the Test button", async () => {
    renderStudio();
    await screen.findByText("Check balance");
    const open = screen.getByRole("button", { name: "Test" });
    open.focus();
    fireEvent.click(open);
    const drawer = screen.getByRole("dialog", { name: "Interactive Test Chat" });
    expect(drawer.contains(document.activeElement)).toBe(true);
    expect(drawer.textContent).toContain("Simulated replies");

    const send = within(drawer).getByRole("button", { name: "Send" });
    send.focus();
    fireEvent.keyDown(drawer, { key: "Tab" });
    expect(document.activeElement).toBe(within(drawer).getByRole("button", { name: "Close test chat" }));

    fireEvent.keyDown(drawer, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Test" }));
  });

  it("gives every button a name and hides decorative icons", async () => {
    const { container } = render(<Studio api={fakeApi(alice)} onSignOut={vi.fn()} />);
    await screen.findByText("Check balance");
    for (const button of container.querySelectorAll("button")) {
      const name = (button.getAttribute("aria-label") ?? button.textContent ?? "").trim();
      expect(name, button.outerHTML).not.toBe("");
    }
    for (const svg of container.querySelectorAll("svg")) {
      expect(svg.getAttribute("aria-hidden") === "true" || svg.getAttribute("aria-label"), svg.outerHTML.slice(0, 80)).toBeTruthy();
    }
    // Buttons that did nothing (notifications, settings, more options, collapse) are gone.
    for (const name of ["Notifications", "Settings", "More options", "Collapse navigation"]) {
      expect(screen.queryByRole("button", { name })).toBeNull();
    }
  });

  it("puts the studio's content in one main landmark and announces status messages", async () => {
    renderStudio();
    await screen.findByText("Check balance");
    expect(screen.getAllByRole("main")).toHaveLength(1);
    expect(screen.getByRole("main").id).toBe("main-content");
    fireEvent.click(screen.getByRole("button", { name: "Add node" }));
    expect(screen.getAllByRole("status").some((el) => el.textContent?.includes("New node added"))).toBe(true);
    // Flow nodes say which one is selected.
    expect(screen.getAllByRole("button", { pressed: true })).toHaveLength(1);
  });
});

describe("Browser storage is documented", () => {
  it("lists every key the studio uses in COOKIES_AND_STORAGE.md", () => {
    for (const key of [SESSION_KEY, TERMS_PENDING_KEY, PRIVACY_CHOICES_KEY, "oidc.user:", "oidc.<random state>"]) {
      expect(cookiesDoc, key).toContain(key);
    }
  });

  it("writes no undocumented keys while signing in, choosing and using the studio", async () => {
    render(<App companySignIn={null} analyticsUrl="https://stats.example.com" />);
    fireEvent.click(screen.getByRole("button", { name: "Reject non-essential" }));
    fireEvent.click(screen.getByRole("checkbox", { name: /accept the Terms of Use/ }));
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => (url.endsWith("/dev/token") ? Response.json({ access_token: "t" }) : url.endsWith("/me") ? Response.json(alice) : Response.json([]))),
    );
    fireEvent.click(screen.getByText("Alice"));
    await waitFor(() => expect(sessionStorage.getItem(SESSION_KEY)).not.toBeNull());
    const keys = [...Object.keys(sessionStorage), ...Object.keys(localStorage)];
    expect(keys.length).toBeGreaterThan(0);
    for (const key of keys) expect(cookiesDoc, `undocumented key ${key}`).toContain(`\`${key}\``);
    expect(document.cookie).toBe("");
  });
});
