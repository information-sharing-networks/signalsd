// handlers package implements the ui-api and public UI route handlers
//
// the /ui-api routes are called by the HTMX library to update page components
// and are not meant to be called directly by the client.
//
// the public UI routes are the standard HTTP routes that render the UI pages (login, register, dashboard etc.)
//
// For every page route that displays a AJAX form, there is a corresponding /ui-api route that is called by the HTMX library when the form is submitted.
// e.g the /admin/isn/signal-types/add page has a form that calls the /ui-api/isn/signal-types/add route.
package handlers
