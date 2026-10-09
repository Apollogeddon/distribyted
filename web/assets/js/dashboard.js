// The speed chart: each refresh of the figures (#stats) adds a point from its data-down and
// data-up attributes, keeping the last minute.
(function () {
    "use strict";

    var POINTS = 30;
    var canvas = document.getElementById("speed-chart");
    if (!canvas || !window.Chart) return;

    var css = getComputedStyle(document.documentElement);
    var colour = function (name, fallback) { return (css.getPropertyValue(name) || "").trim() || fallback; };
    var down = colour("--chart-down", "#38bdf8");
    var up = colour("--chart-up", "#4ade80");
    var muted = colour("--text-muted", "#94a3b8");

    // IEC units, as everywhere else on the page
    function rate(n) {
        var units = ["B", "KiB", "MiB", "GiB", "TiB"];
        var i = 0;
        while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
        return (i === 0 || n >= 10 ? Math.round(n) : n.toFixed(1)) + " " + units[i] + "/s";
    }

    var labels = [];
    var downData = [];
    var upData = [];
    function line(label, data, c) {
        return {
            label: label, data: data, borderColor: c, backgroundColor: "transparent",
            borderWidth: 2, lineTension: 0.25, pointRadius: 0, pointHoverRadius: 4, pointHitRadius: 8
        };
    }

    var chart = new Chart(canvas.getContext("2d"), {
        type: "line",
        data: { labels: labels, datasets: [line("Download", downData, down), line("Upload", upData, up)] },
        options: {
            animation: false,
            responsive: true,
            maintainAspectRatio: false,
            legend: { display: false },
            scales: {
                xAxes: [{ display: false }],
                yAxes: [{
                    gridLines: { color: "rgba(148, 163, 184, 0.15)", zeroLineColor: "rgba(148, 163, 184, 0.3)" },
                    ticks: {
                        beginAtZero: true, fontColor: muted, maxTicksLimit: 5,
                        // ticks Chart.js places between round numbers would repeat a rounded label
                        callback: function (v, i, all) {
                            var label = rate(v);
                            return i > 0 && rate(all[i - 1]) === label ? null : label;
                        }
                    }
                }]
            },
            tooltips: {
                mode: "index", intersect: false, displayColors: true,
                callbacks: {
                    label: function (item, data) { return data.datasets[item.datasetIndex].label + ": " + rate(item.yLabel); }
                }
            }
        }
    });

    function add(stats) {
        labels.push(new Date().toLocaleTimeString());
        downData.push(Number(stats.dataset.down) || 0);
        upData.push(Number(stats.dataset.up) || 0);
        if (labels.length > POINTS) { labels.shift(); downData.shift(); upData.shift(); }
        chart.update();
    }

    var first = document.getElementById("stats");
    if (first) add(first);
    document.body.addEventListener("htmx:afterSettle", function (e) {
        var stats = document.getElementById("stats");
        if (stats && (e.target === stats || e.target.contains(stats))) add(stats);
    });
})();
