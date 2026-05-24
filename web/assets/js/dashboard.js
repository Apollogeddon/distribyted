GeneralChart.init();

Distribyted.dashboard = {
    _cacheChart: new CacheChart("main-cache-chart", "Cache disk"),
    loadView: function () {
        fetch('/api/status')
            .then(function (response) {
                if (response.ok) {
                    Distribyted.offline.hide();
                    return response.json();
                } else {
                    Distribyted.offline.show();
                }
            }).then(function (stats) {
                if (!stats) return;
                var download = stats.torrentStats.downloadedBytes / stats.torrentStats.timePassed;
                var upload = stats.torrentStats.uploadedBytes / stats.torrentStats.timePassed;

                GeneralChart.update(download, upload);

                Distribyted.dashboard._cacheChart.update(stats.cacheFilled, stats.cacheCapacity - stats.cacheFilled);

                document.getElementById("general-download-speed").innerText =
                    Humanize.ibytes(download, 1024) + "/s";

                document.getElementById("general-upload-speed").innerText =
                    Humanize.ibytes(upload, 1024) + "/s";

                document.getElementById("kpi-download").innerText = Humanize.ibytes(download, 1024) + "/s";
                document.getElementById("kpi-upload").innerText = Humanize.ibytes(upload, 1024) + "/s";
                var cachePct = stats.cacheCapacity > 0
                    ? Math.round(stats.cacheFilled / stats.cacheCapacity * 100) + "%"
                    : "N/A";
                document.getElementById("kpi-cache-pct").innerText = cachePct;
                document.getElementById("kpi-cache-items").innerText = stats.cacheItems;
            })
            .catch(function () {
                Distribyted.offline.show();
            });
    }
}
