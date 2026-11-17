import { Fragment, type ReactNode } from "react";
import { legalHrefForFile } from "./legal";

// A small Markdown renderer for the documents in docs/legal: headings, paragraphs,
// lists, tables, block quotes, rules and code blocks, with bold, code and links inline.
// It builds React elements (never HTML strings), so nothing in a document can run as code.

/** Where a Markdown link may point: other legal documents, the web, email, or this page. */
function safeHref(href: string): { href: string; external: boolean } | null {
  const legal = legalHrefForFile(href.split("#")[0]);
  if (legal) return { href: legal, external: false };
  if (/^https?:\/\//i.test(href)) return { href, external: true };
  if (/^mailto:/i.test(href) || href.startsWith("#")) return { href, external: false };
  return null; // relative paths to files the studio does not serve
}

const INLINE = /(\*\*[^*]+\*\*|`[^`]+`|\[\[[^\]]+\]\]|\[[^\]]+\]\([^)\s]+\)|\*[^*\s][^*]*\*)/g;

export function inline(text: string, keyPrefix = "i"): ReactNode[] {
  const parts = text.split(INLINE).filter((p) => p !== "");
  return parts.map((part, i) => {
    const key = `${keyPrefix}-${i}`;
    if (part.startsWith("**") && part.endsWith("**")) return <strong key={key}>{inline(part.slice(2, -2), key)}</strong>;
    if (part.startsWith("`") && part.endsWith("`")) return <code key={key}>{part.slice(1, -1)}</code>;
    if (part.startsWith("[[") && part.endsWith("]]")) {
      // Not filled in yet: shown as such, never hidden.
      return (
        <mark key={key} className="placeholder" title="To be completed before publication">
          {part}
        </mark>
      );
    }
    const link = /^\[([^\]]+)\]\(([^)\s]+)\)$/.exec(part);
    if (link) {
      const target = safeHref(link[2]);
      if (!target) return <Fragment key={key}>{inline(link[1], key)}</Fragment>;
      return target.external ? (
        <a key={key} href={target.href} target="_blank" rel="noopener noreferrer">
          {inline(link[1], key)}
          <span className="sr-only"> (opens in a new tab)</span>
        </a>
      ) : (
        <a key={key} href={target.href}>
          {inline(link[1], key)}
        </a>
      );
    }
    if (part.length > 2 && part.startsWith("*") && part.endsWith("*")) return <em key={key}>{inline(part.slice(1, -1), key)}</em>;
    return <Fragment key={key}>{part}</Fragment>;
  });
}

const splitRow = (line: string) =>
  line
    .trim()
    .replace(/^\||\|$/g, "")
    .split("|")
    .map((cell) => cell.trim());

const isTableSeparator = (line: string) => /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$/.test(line);

/** Heading text to an id, for links within a page. */
export const slugify = (text: string) =>
  text
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, "")
    .trim()
    .replace(/\s+/g, "-");

export function Markdown({ source, headingOffset = 0 }: { source: string; headingOffset?: number }) {
  const lines = source.replace(/<!--[\s\S]*?-->/g, "").replace(/\r\n/g, "\n").split("\n");
  const blocks: ReactNode[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    const key = `b-${i}`;

    if (line.trim() === "") {
      i++;
      continue;
    }

    if (line.startsWith("```")) {
      const code: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith("```")) code.push(lines[i++]);
      i++;
      blocks.push(
        <pre key={key}>
          <code>{code.join("\n")}</code>
        </pre>,
      );
      continue;
    }

    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      const level = Math.min(heading[1].length + headingOffset, 6);
      const Tag = `h${level}` as "h1";
      blocks.push(
        <Tag key={key} id={slugify(heading[2])}>
          {inline(heading[2], key)}
        </Tag>,
      );
      i++;
      continue;
    }

    if (/^-{3,}\s*$/.test(line)) {
      blocks.push(<hr key={key} />);
      i++;
      continue;
    }

    if (line.trim().startsWith("|") && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
      const header = splitRow(line);
      const rows: string[][] = [];
      i += 2;
      while (i < lines.length && lines[i].trim().startsWith("|")) rows.push(splitRow(lines[i++]));
      blocks.push(
        <div key={key} className="table-scroll" role="region" aria-label={header.filter(Boolean).join(", ") || "Table"} tabIndex={0}>
          <table>
            <thead>
              <tr>
                {header.map((cell, c) => (
                  <th key={c} scope="col">
                    {inline(cell, `${key}-h${c}`)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, r) => (
                <tr key={r}>
                  {row.map((cell, c) => (
                    <td key={c}>{inline(cell, `${key}-${r}-${c}`)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }

    if (line.startsWith(">")) {
      const quote: string[] = [];
      while (i < lines.length && lines[i].startsWith(">")) quote.push(lines[i++].replace(/^>\s?/, ""));
      blocks.push(
        <blockquote key={key}>
          <p>{inline(quote.join(" "), key)}</p>
        </blockquote>,
      );
      continue;
    }

    const listItem = /^\s*(?:([-*])|(\d+)\.)\s+(.*)$/;
    if (listItem.test(line)) {
      const ordered = /^\s*\d+\./.test(line);
      const items: string[] = [];
      while (i < lines.length && listItem.test(lines[i]) && /^\s*\d+\./.test(lines[i]) === ordered) {
        let item = listItem.exec(lines[i++])![3];
        // A wrapped line that is not a new item belongs to the current one.
        while (i < lines.length && /^\s{2,}\S/.test(lines[i]) && !listItem.test(lines[i])) item += " " + lines[i++].trim();
        items.push(item);
      }
      const List = ordered ? "ol" : "ul";
      blocks.push(
        <List key={key}>
          {items.map((item, n) => (
            <li key={n}>{inline(item, `${key}-${n}`)}</li>
          ))}
        </List>,
      );
      continue;
    }

    const paragraph: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !/^(#{1,6}\s|```|>|\s*([-*]|\d+\.)\s|-{3,}\s*$)/.test(lines[i]) &&
      !lines[i].trim().startsWith("|")
    ) {
      paragraph.push(lines[i++].trim());
    }
    if (paragraph.length === 0) {
      // A line no rule matched (for example a lone "|"): show it as text.
      paragraph.push(lines[i++].trim());
    }
    blocks.push(<p key={key}>{inline(paragraph.join(" "), key)}</p>);
  }

  return <>{blocks}</>;
}
