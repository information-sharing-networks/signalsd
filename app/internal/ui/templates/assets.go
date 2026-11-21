package templates

// htmxScriptPath is the locally bundled copy of htmx (app/web/static/js).
//
// htmx is served from the UI's own origin rather than a CDN, so the pages do not depend on a third party being available
// and the Content-Security-Policy can restrict scripts to 'self'.
//
// To upgrade: download dist/htmx.min.js for the new version from the npm registry (check the tarball against the
// registry's published integrity hash), save it as web/static/js/htmx-<version>.min.js, delete the old file and update
// this path. The version in the file name ensures browsers do not keep using a cached copy of the old version.
const htmxScriptPath = "/static/js/htmx-2.0.7.min.js"
