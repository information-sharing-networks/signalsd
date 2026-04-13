// package server contains the handlers that render the UI pages and HTMX components.
//
// Pages and components are constructed using templ templates (see the templates package).
//
// **UI Page**
//
// The *Page handlers render the pages at the UI's public url routes (/login, /admin/* etc)
//
// **HTMX components**
//
// the HTMX javascript library included in the UI pages intercepts clicks amd form submissions and -
// when an action is triggered - sends an AJAX request that receives a fragment of HTML
// (e.g., a single table row or a button state) in return.
// The library then injects the partial HTML directly into the existing DOM based on the hx-target
// and hx-swap attributes in the page - this avoids full page refreshes when updating the page contents.
//
// the handlers used to return the html fragments are all routed by internal /ui-api/* urls
//
// **Retriving data from signalsd**
//
// When a handler needs data from the backend service it should use the UI client package to call the signalsd API.
//
// **inline js**
//
// Note the Content Security Policy on this app blocks inline javascripts and stylesheets -
// where the templates need to use JS/css add it to the app/web/js and app/web/css directories respectively.
package server
