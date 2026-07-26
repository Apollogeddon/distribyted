Distribyted.links = {
    _template: null,

    _getTemplate: function () {
        if (this._template != null) {
            return this._template
        }

        const tTemplate = fetch('/assets/templates/links.html')
            .then((response) => {
                if (response.ok) {
                    return response.text();
                } else {
                    Distribyted.message.error('Error getting data from server. Response: ' + response.status);
                }
            })
            .then((t) => {
                return Handlebars.compile(t);
            })
            .catch(error => {
                Distribyted.message.error('Error getting links template: ' + error.message);
            });

        this._template = tTemplate;
        return tTemplate;
    },

    _getLinksJson: function () {
        return fetch('/api/links')
            .then(function (response) {
                if (Distribyted.auth.handleResponse(response)) return;
                if (response.ok) {
                    Distribyted.offline.hide();
                    return response.json();
                } else {
                    Distribyted.offline.show();
                }
            }).then(function (links) {
                return links;
            })
            .catch(function () {
                Distribyted.offline.show();
            });
    },

    confirmDelete: function (newPath) {
        if (!confirm('Delete link "' + newPath + '"?\n\nIf this is the last reference to a torrent, the torrent will be removed too.')) return;
        this.deleteLink(newPath);
    },

    deleteLink: function (newPath) {
        var url = '/api/links' + newPath.split('/').map(encodeURIComponent).join('/');

        return fetch(url, {
            method: 'DELETE'
        })
            .then(function (response) {
                if (Distribyted.auth.handleResponse(response)) return;
                if (response.ok) {
                    Distribyted.message.info('Link deleted.')
                    Distribyted.links.loadView();
                } else {
                    response.json().then(json => {
                        Distribyted.message.error('Error deleting link. Response: ' + json.error)
                    })
                }
            })
            .catch(function (error) {
                Distribyted.message.error('Error deleting link: ' + error.message)
            });
    },

    loadView: function () {
        this._getTemplate()
            .then(t =>
                this._getLinksJson().then(links => {
                    document.getElementById('template_target').innerHTML = t(links);
                })
            );
    }
}

$("#new-link").submit(function (event) {
    event.preventDefault();

    let oldPath = $("#link-old-path").val().trim()
    let newPath = $("#link-new-path").val().trim()
    let newPathInput = document.getElementById("link-new-path")
    let feedbackEl = document.getElementById("link-feedback")

    if (!oldPath.startsWith("/") || !newPath.startsWith("/")) {
        newPathInput.classList.add("is-invalid")
        feedbackEl.textContent = "Both paths must be absolute (start with /)"
        return
    }
    newPathInput.classList.remove("is-invalid")
    feedbackEl.textContent = ""

    let body = JSON.stringify({ old_path: oldPath, new_path: newPath })

    document.getElementById("submit_link_loading").style = "display:block"

    fetch('/api/links', {
        method: 'POST',
        body: body
    })
        .then(function (response) {
            if (Distribyted.auth.handleResponse(response)) return;
            if (response.ok) {
                Distribyted.message.info('New link added.')
                document.getElementById("link-old-path").value = ""
                document.getElementById("link-new-path").value = ""
                Distribyted.links.loadView();
            } else {
                response.json().then(json => {
                    Distribyted.message.error('Error adding new link. Response: ' + json.error)
                }).catch(function (error) {
                    Distribyted.message.error('Error adding new link: ' + response.status)
                });
            }
        })
        .catch(function (error) {
            Distribyted.message.error('Error adding link: ' + error.message)
        }).then(function () {
            document.getElementById("submit_link_loading").style = "display:none"
        });
});
