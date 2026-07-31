Handlebars.registerHelper("torrent_status", function (chunks, totalPieces, pieceSize) {
    const pieceStatus = {
        "H": { class: "bg-warning", label: "checking" },
        "P": { class: "bg-info", label: "partial" },
        "C": { class: "bg-success", label: "downloaded" },
        "W": { class: "bg-transparent", label: "waiting" },
        "?": { class: "bg-danger", label: "errored" },
    };

    // Torrent metadata (piece layout) hasn't arrived yet — chunks/totalPieces
    // are unset until then, and totalPieces === 0 would otherwise divide by
    // zero below.
    if (!chunks || !totalPieces) {
        return '<div class="progress mb-1"><div class="progress-bar progress-bar-striped progress-bar-animated bg-secondary" role="progressbar" style="width: 100%"></div></div>'
            + '<div class="text-muted" style="font-size:0.75rem">fetching metadata&hellip;</div>';
    }

    let completePieces = 0;
    let pieceIndex = 0;
    const chunksAsHTML = chunks.map(chunk => {
        const startPiece = pieceIndex;
        pieceIndex += chunk.numPieces;
        const endPiece = pieceIndex - 1;

        const percentage = chunk.numPieces * 100 / totalPieces;
        const pcMeta = pieceStatus[chunk.status] || { class: "bg-secondary", label: chunk.status };
        if (chunk.status === "C") completePieces += chunk.numPieces;

        const div = document.createElement("div");
        div.className = "progress-bar " + pcMeta.class;
        div.setAttribute("role", "progressbar");
        div.setAttribute("data-toggle", "tooltip");
        div.setAttribute("data-placement", "top");
        div.setAttribute("title", "pieces " + startPiece + "–" + endPiece + " (" + pcMeta.label + ")");
        div.style.cssText = "width: " + percentage + "%";

        return div.outerHTML;
    });

    const pct = Math.round(completePieces * 100 / totalPieces);
    const sizeLabel = pieceSize ? Humanize.bytes(completePieces * pieceSize, 1024) : "";

    return '<div class="progress mb-1 piece-bar">' + chunksAsHTML.join("") + '</div>'
        + '<div class="text-muted" style="font-size:0.75rem">' + pct + '% &middot; ' + completePieces + ' / ' + totalPieces + ' pieces'
        + (sizeLabel ? ' &middot; ' + sizeLabel : '') + '</div>';
});

Handlebars.registerHelper("torrent_info", function (peers, seeders, pieceSize) {
    const MB = 1048576;

    var messages = [];

    var errorLevels = [];
    const seedersMsg = "- Number of seeders is too low (" + seeders + ")."
    if (seeders < 2) {
        errorLevels[0] = 2;
        messages.push(seedersMsg);
    } else if (seeders >= 2 && seeders < 4) {
        errorLevels[0] = 1;
        messages.push(seedersMsg);
    } else {
        errorLevels[0] = 0;
    }

    const pieceSizeMsg = "- Piece size is too big (" + Humanize.bytes(pieceSize, 1024) + "). Recommended size is 1MB or less."
    if (pieceSize <= MB) {
        errorLevels[1] = 0;
    } else if (pieceSize > MB && pieceSize < (MB * 4)) {
        errorLevels[1] = 1;
        messages.push(pieceSizeMsg);
    } else {
        errorLevels[1] = 2;
        messages.push(pieceSizeMsg);
    }

    const level = ["text-success", "text-warning", "text-danger"];
    const icon = ["mdi-check", "mdi-alert", "mdi-alert-octagram"];
    const errIndex = Math.max(...errorLevels);

    return `
    <div class="d-flex flex-column">
        <div class="font-weight-bold" style="font-size:0.9rem">
            <span class="text-info" title="Peers"><i class="mdi mdi-account-multiple"></i> ${peers}</span>
            <span class="mx-1 text-muted">|</span>
            <span class="text-success" title="Seeders"><i class="mdi mdi-seed"></i> ${seeders}</span>
        </div>
        <div class="text-muted" style="font-size:0.8rem">
            <i class="mdi mdi-puzzle"></i> ${Humanize.bytes(pieceSize, 1024)} chunks
        </div>
        <div class="${level[errIndex]} mt-1" style="font-size:0.8rem" title="${messages.join('\n')}">
            <i class="mdi ${icon[errIndex]}"></i> ${errIndex === 0 ? "Healthy" : "Warning"}
        </div>
    </div>`;
});

Distribyted.routes = {
    _template: null,

    _getTemplate: function () {
        if (this._template != null) {
            return this._template
        }

        const tTemplate = fetch('/assets/templates/routes.html')
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
                Distribyted.message.error('Error getting routes template: ' + error.message);
            });

        this._template = tTemplate;
        return tTemplate;
    },

    _getRoutesJson: function () {
        return fetch('/api/routes')
            .then(function (response) {
                if (Distribyted.auth.handleResponse(response)) return;
                if (response.ok) {
                    Distribyted.offline.hide();
                    return response.json();
                } else {
                    Distribyted.offline.show();
                }
            }).then(function (routes) {
                return routes;
            })
            .catch(function () {
                Distribyted.offline.show();
            });
    },

    confirmDelete: function (route, torrentHash, torrentName) {
        Distribyted.confirm({
            title: 'Delete torrent',
            body: 'Delete "' + torrentName + '"? This will remove the torrent and cannot be undone.',
            confirmLabel: 'Delete',
            danger: true
        }).then((ok) => {
            if (ok) this.deleteTorrent(route, torrentHash);
        });
    },

    deleteTorrent: function (route, torrentHash) {
        var url = '/api/routes/' + route + '/torrent/' + torrentHash

        return fetch(url, {
            method: 'DELETE'
        })
            .then(function (response) {
                if (Distribyted.auth.handleResponse(response)) return;
                if (response.ok) {
                    Distribyted.message.info('Torrent deleted.')
                    Distribyted.routes.loadView();
                } else {
                    response.json().then(json => {
                        Distribyted.message.error('Error deletting torrent. Response: ' + json.error)
                    })
                }
            })
            .catch(function (error) {
                Distribyted.message.error('Error deletting torrent: ' + error.message)
            });
    },

    loadView: function () {
        this._getTemplate()
            .then(t =>
                this._getRoutesJson().then(routes => {
                    document.getElementById('template_target').innerHTML = t(routes);
                })
            );
    }
}

$("#new-magnet").submit(function (event) {
    event.preventDefault();

    let route = $("#route-string :selected").val()
    let magnet = $("#magnet-url").val().trim()
    let magnetInput = document.getElementById("magnet-url")
    let feedbackEl = document.getElementById("magnet-feedback")

    if (!magnet.startsWith("magnet:?")) {
        magnetInput.classList.add("is-invalid")
        feedbackEl.textContent = "Please enter a valid magnet link (must start with magnet:?)"
        return
    }
    magnetInput.classList.remove("is-invalid")
    feedbackEl.textContent = ""

    let url = '/api/routes/' + route + '/torrent'
    let body = JSON.stringify({ magnet: magnet })

    document.getElementById("submit_magnet_loading").style = "display:block"

    fetch(url, {
        method: 'POST',
        body: body
    })
        .then(function (response) {
            if (Distribyted.auth.handleResponse(response)) return;
            if (response.ok) {
                Distribyted.message.info('New magnet added.')
                document.getElementById("magnet-url").value = ""
                Distribyted.routes.loadView();
            } else {
                response.json().then(json => {
                    Distribyted.message.error('Error adding new magnet. Response: ' + json.error)
                }).catch(function (error) {
                    Distribyted.message.error('Error adding new magnet: ' + response.status)
                });
            }
        })
        .catch(function (error) {
            Distribyted.message.error('Error adding torrent: ' + error.message)
        }).then(function () {
            document.getElementById("submit_magnet_loading").style = "display:none"
        });
});