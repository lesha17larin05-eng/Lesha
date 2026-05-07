// Helper to extract reusable parts (page-specific styles + body content
// minus the original nav/footer) from the legacy designed HTML in src/legacy/.
//
// The legacy files are full <html> documents. We render them inside SiteLayout
// using the shared Header/Footer, so the original nav/footer must be stripped.

export type LegacyParts = {
  styles: string; // contents of <style>...</style>
  body: string;   // body inner HTML, with original nav/footer removed
};

const reStyle = /<style[^>]*>([\s\S]*?)<\/style>/i;
const reBody = /<body[^>]*>([\s\S]*?)<\/body>/i;
const reNav = /<nav[\s\S]*?<\/nav>/i;
const reFooter = /<footer[\s\S]*?<\/footer>/i;
const reAnchorNav = /<div\s+class=["']anchor-nav["'][\s\S]*?<\/div>/i;

export function extractLegacy(raw: string): LegacyParts {
  const styles = reStyle.exec(raw)?.[1] ?? '';
  let body = reBody.exec(raw)?.[1] ?? '';
  body = body.replace(reNav, '');
  body = body.replace(reFooter, '');
  body = body.replace(reAnchorNav, '');
  return { styles, body };
}
