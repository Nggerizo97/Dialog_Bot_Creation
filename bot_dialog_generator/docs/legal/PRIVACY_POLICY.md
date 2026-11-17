<!--
DRAFT TEMPLATE. Not legal advice. Review with counsel before publishing.
Replace every [[PLACEHOLDER]]; docs/legal/README.md lists them.
Written from the product's behavior as checked in the code on 16 Nov 2026. Text in
[square brackets] applies only once AI answers are switched on (milestone 5).
In Colombia this policy is the Política de Tratamiento de la Información; the chat
widget's authorization step (require-authorization) collects the prior, express
authorization of Ley 1581, Art. 9.
-->

# Privacy Policy

**Effective date:** [[EFFECTIVE_DATE]] · **Version:** [[POLICY_VERSION]]

This policy explains what **[[COMPANY_LEGAL_NAME]]** ("we", "us") does with personal information in Bot_Dialog_Generator. It covers two groups of people:

- **People who chat** with an assistant on a website or channel we run (part A).
- **Staff** who sign in to the Bot_Dialog_Generator studio to build assistants (part B).

## Who is responsible

[[COMPANY_LEGAL_NAME]], [[REGISTERED_ADDRESS]], tax ID [[TAX_ID]], is responsible for your information (the "data controller", or *responsable del tratamiento* in Colombia). See [Business and legal contact](BUSINESS_INFORMATION.md) for every way to reach us.

- Privacy requests: [[PRIVACY_EMAIL]]
- Data Protection Officer: [[DPO_CONTACT]]

## How each area's data is kept separate

The platform serves several areas of our organization (for example Human resources and Customer service). Each area has its own private **workspace**, and an assistant belongs to exactly one of them. This separation is enforced by the system, not only by policy:

1. **The chat channel decides the workspace, not your browser.** The chat gateway is configured on our servers with the workspace it serves (`WEBCHAT_WORKSPACE_ID`, and per-widget bindings later). Anything a page or browser sends to name another workspace is ignored.
2. **The database refuses cross-area access.** Every table that holds an area's data, including consent records, campaign records and deletion requests, is protected by PostgreSQL row-level security. The application connects as a database role that owns no tables, declares one workspace per transaction, and cannot read or change another workspace's rows even if a query is wrong. Automated tests prove this on every change.
3. **Staff see only their own areas.** Members of other areas cannot see an area's assistants, conversations or members. Platform administrators can see every area's configuration, and each such access is recorded in an audit log. Administrators cannot read conversations except to investigate a reported fault, with a stated reason, for a limited time, and recorded.

---

## Part A: people who chat with an assistant

### You are chatting with an automated assistant

The chat says so before you write anything. It follows conversation flows designed by our teams[, and it can use artificial intelligence to answer from our own documents]. Wherever the chat offers it, you can ask for a person.

**Please do not share sensitive information in the chat**, such as passwords, card numbers, health information or identity documents, unless the assistant asks for it as part of a secure process.

### What we collect, and what we do not

| Information | Example | Why | Where it is kept |
|---|---|---|---|
| The messages you type and the options you choose | "How many vacation days do I have?" | To answer you | Your browser tab; [[the conversation service, once enabled]] |
| A random conversation ID | `usr-4f9a1c7e2b…` (32 random characters) | To keep your conversation together | Your browser tab; our servers, with the items below |
| Proof of what you agreed to | "accepted policy 2026-11, analytics: no" | To show that you gave (or refused) consent | Our database, until you delete it |
| Whether you confirmed your age, or a guardian gave permission | "age confirmed: yes" | Assistants with an age limit | Our database, with the proof of consent |
| Technical data needed to deliver the chat | IP address and time of each request | Security and delivery | Our hosting provider's logs, [[RETENTION_LOGS]] |

**We do not collect** your name, email address or phone number through the chat unless an assistant asks for them as part of a request you made. The conversation ID is random and says nothing about you. The chat does not read your location, contacts, camera or files, and the system has no field to receive them.

**What the website hosting the chat can see.** The chat tells the page that a message was sent, but not what it said, unless the website owner switches that on (`expose-message-text`). It does not give the page your conversation ID.

### Optional choices

Two things are optional and **off unless you switch them on**. The chat works the same either way, and you can change your mind at any time under **Chat options → Privacy choices**.

| Choice | What it does | What it never includes |
|---|---|---|
| Anonymous usage statistics | Counts events such as "a message was sent" or "an option was chosen" | Your messages, your conversation ID, your IP address |
| Campaign attribution | Remembers which campaign link (for example `utm_campaign=spring`) brought you to the page | Anything else in the address, such as account numbers or search terms |

Our analytics service refuses events without your opt-in, refuses anything that looks like free text, and keeps only totals. It has no record about you to look up.

### Why we use your information

| Purpose | Legal basis (EU/UK GDPR) | Colombia (Ley 1581) |
|---|---|---|
| Answer your questions and complete what you ask for | Steps you requested (Art. 6(1)(b)), or our legitimate interest in answering enquiries (Art. 6(1)(f)) | Your authorization (Art. 9), collected before the first message where required |
| Hand the conversation to a person when you ask | Same as above | Same |
| Keep the service secure and investigate reported faults | Legitimate interest (Art. 6(1)(f)) | Your authorization |
| Usage statistics and campaign attribution (optional) | Your consent (Art. 6(1)(a)), which you can withdraw | Your authorization, which you can revoke |

We do not sell your information. We do not use it for advertising, and we do not make decisions with legal or similarly significant effects about you based solely on automated processing.

### [How AI answers use your messages]

[When an assistant uses AI, the text of your question is sent to our AI model provider, [[AI_PROVIDER_AND_REGION]], together with passages from documents that belong to the same area (this is called retrieval-augmented generation). The search for those passages is limited on our servers to that area's documents; it is never chosen by the text of your message or by the AI.]

[- **No training on your data.** We do not use your conversations to train or fine-tune AI models, and our agreement with the provider forbids it from using them to train its models.]
[- **No answer without a source.** If none of the area's documents is relevant, the AI is not called and the assistant says it cannot answer.]
[- **Sensitive details removed first** where the area's settings require it.]

### Children

Assistants are not intended for children under [[AGE]] unless they say otherwise. When an assistant is meant for children, it asks for a parent's or guardian's permission before the chat starts, and the team that publishes it must confirm that a parental-consent process is in place. If you believe a child shared information without that permission, write to [[PRIVACY_EMAIL]] and we will delete it.

---

## Part B: staff who use the studio

| Information | Example | Source |
|---|---|---|
| Your work identity | User ID, name and work email from your organization's sign-in service (Microsoft Entra ID or Amazon Cognito) | The sign-in service, when you sign in |
| Your groups that are assigned to the studio | "Legal editors" | The sign-in service, managed by IT |
| Your access in each area | Owner of Legal, analyst in Customer service | Area owners and platform administrators |
| A record of sensitive actions | "Published version 4 of HR helpdesk on 7 Oct 2026 at 10:42"; "accepted the terms, version 2026-11" | The studio, automatically |
| Content you create | Bot flows, messages, draft versions | You |
| Technical data | IP address, browser, time of requests | Your browser, our hosting provider |

| Purpose | Legal basis (EU/UK GDPR) | Colombia (Ley 1581) |
|---|---|---|
| Sign you in and show you only the areas you belong to | Employment or service contract (Art. 6(1)(b)) | Your authorization, given as part of your employment relationship |
| Keep an audit log of publishing, access changes, administrator access and deletion requests | Legitimate interest in security and accountability (Art. 6(1)(f)) | Same |
| Usage statistics in the studio (optional, off until you allow it) | Your consent (Art. 6(1)(a)) | Your authorization |

[[CONFIRM WITH HR/LEGAL: The audit log is not used to evaluate individual performance. It is used only for security, compliance and incident investigation.]]

Members of other areas cannot see that you belong to an area they are not part of.

---

## Storage in your browser

The chat and the studio set **no cookies**. They keep a few values in your browser's session storage, which is deleted when you close the tab. The full list, with each key and why it is needed, is in [Cookies and browser storage](COOKIES_AND_STORAGE.md).

## Who can see your information

- **The team that runs the assistant**, and only that team (see "How each area's data is kept separate").
- **Platform administrators**, as described above, always recorded in the audit log.
- **Service providers acting on our instructions**: our hosting provider, [[HOSTING_PROVIDER_AND_REGION]]; our staff sign-in service, [[IDENTITY_PROVIDER — Microsoft Entra ID or Amazon Cognito in [[REGION]]]][, and our AI model provider, [[AI_PROVIDER_AND_REGION]]]. The full list of software and services the platform uses is in [Third-party software and services](THIRD_PARTY_INVENTORY.md).
- **Authorities**, when the law requires it.

## International transfers

Your information is processed in [[HOSTING_PROVIDER_AND_REGION]][ and [[AI_PROVIDER_AND_REGION]]]. When it leaves [[JURISDICTION]], we use the safeguards the law requires [[for example: countries recognized as adequate, standard contractual clauses, or the exceptions in Art. 26 of Ley 1581]].

## How long we keep it

| Information | Kept for | Then |
|---|---|---|
| Conversation in your browser | Until you close the tab, end the chat, or delete it | Deleted by your browser |
| Conversation on our servers [[once stored]] | [[RETENTION_TRANSCRIPTS — proposed: 90 days]] | Deleted |
| Proof of consent | While the conversation exists, and up to [[RETENTION_CONSENT — proposed: 2 years]] as evidence of the authorization | Deleted |
| Campaign delivery records | [[RETENTION_CAMPAIGNS — proposed: 1 year]] | Deleted |
| Do-not-contact list (after you unsubscribe or ask for deletion) | As long as campaigns are sent | It holds only a one-way code, never your address |
| Record of a deletion request | [[RETENTION_REQUESTS — proposed: 3 years]], as proof that we acted | It holds only a one-way code |
| Usage statistics | Totals only; nothing about a person | — |
| Technical logs | [[RETENTION_LOGS — proposed: 30 days]] | Deleted |
| Audit log | [[RETENTION_AUDIT — proposed: 1 year]] | Deleted |
| Staff access to an area | Until you are removed, or your sign-in account or group ends | Deleted |

## Your rights

Depending on where you live, you can **know** what we hold about you and get a copy, **correct** it, **delete** it or **revoke** your authorization, **object** to or **restrict** some uses, and **complain** to [[SUPERVISORY_AUTHORITY]] (the Superintendencia de Industria y Comercio in Colombia).

### Delete your data yourself

- **In the chat:** open **Chat options → Delete my data**. This deletes the conversation in your browser and asks our servers to delete what they hold about it: the proof of consent at once, and the conversation, statistics and AI references through the services that keep them. You get a reference number. If our servers cannot be reached, nothing is reported as deleted and you are asked to try again.
- **Staff, in the studio:** open **Privacy → Request deletion of my data**. A platform administrator carries it out. Your access to every area is removed, except where you are an area's only owner, which is kept until someone else becomes owner. Audit log entries are kept for the period above, and the bots you built stay with the organization.
- **Campaign emails:** every email has a one-click **Unsubscribe** link. It takes effect immediately; nothing more is needed.

You can also write to [[PRIVACY_EMAIL]]. If you contact us instead of using the chat, tell us the website and the approximate date and time of the conversation; an area owner can then delete it from the studio after checking that the request is yours. We answer within [[the legal deadline: 10 business days for queries and 15 for claims in Colombia; one month in the EU/UK]].

## Changes to this policy

We publish changes here with a new effective date and version. Staff are asked to accept the new version at their next sign-in, and the chat asks again before significant changes apply.
