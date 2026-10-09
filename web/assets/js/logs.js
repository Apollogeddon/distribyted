Distribyted.logs = {
    loadView: function () {
        fetch("/api/log")
            .then(response => {
                if (Distribyted.auth.handleResponse(response)) return;
                if (response.ok) {
                    return response.body.getReader();
                } else {
                    response.json().then(json => {
                        Distribyted.message.error('Error getting logs from server. Error: ' + json.error);
                    }).catch(error => {
                        Distribyted.message.error('Error getting logs from server. Error: ' + error);
                    })
                }
            })
            .then(reader => {
                var decoder = new TextDecoder()
                var lastString = ''
                reader.read().then(function processText({ done, value }) {
                    if (done) {
                        return;
                    }

                    const string = `${lastString}${decoder.decode(value)}`
                    const lines = string.split(/\r\n|[\r\n]/g)
                    this.lastString = lines.pop() || ''

                    var scrollEl = document.getElementById("log-scroll")
                    lines.forEach(element => {
                        try {
                            var json = JSON.parse(element)
                            // log fields carry request paths and torrent names, so every
                            // value goes in as text, never as HTML
                            var properties = document.createElement("td")
                            for (let [key, value] of Object.entries(json)) {
                                if (key == "level" || key == "component" || key == "message" || key == "time") {
                                    continue
                                }

                                var name = document.createElement("b")
                                name.textContent = key
                                properties.append(name, "=" + (typeof value === "object" ? JSON.stringify(value) : value) + " ")
                            }

                            var tableClass = "table-primary"
                            switch (json.level) {
                                case "info":
                                    tableClass = ""
                                    break;
                                case "error":
                                    tableClass = "table-danger"
                                    break;
                                case "warn":
                                    tableClass = "table-warning"
                                    break;
                                case "debug":
                                    tableClass = "table-info"
                                    break;
                                default:
                                    break;
                            }
                            var level = json.level || "info"
                            var row = document.createElement("tr")
                            row.className = tableClass
                            row.setAttribute("data-level", level)
                            ;[new Date(json.time*1000).toLocaleString(), level, json.component, json.message].forEach(function (text) {
                                var cell = document.createElement("td")
                                cell.textContent = text === undefined ? "" : text
                                row.appendChild(cell)
                            })
                            row.appendChild(properties)

                            var atTop = !scrollEl || scrollEl.scrollTop < 5
                            var prevHeight = scrollEl ? scrollEl.scrollHeight : 0
                            document.getElementById("log_table").appendChild(row)
                            if (!atTop && scrollEl) {
                                scrollEl.scrollTop += scrollEl.scrollHeight - prevHeight
                            }
                        } catch (err) {
                            console.log(err);
                        }
                    });

                    return reader.read().then(processText);
                }).catch(err => console.log(err));
            }).catch(err => console.log(err));
    }
}

document.addEventListener("DOMContentLoaded", function () {
    var filters = document.getElementById("log-filters")
    if (!filters) return
    filters.addEventListener("click", function (e) {
        var btn = e.target.closest("[data-filter]")
        if (!btn) return
        filters.querySelectorAll(".btn").forEach(function (b) { b.classList.remove("active") })
        btn.classList.add("active")
        var level = btn.getAttribute("data-filter")
        var table = document.getElementById("log_table")
        table.className = level === "all" ? "" : "filter-" + level
    })
})
