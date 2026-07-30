Distribyted.files = {
    // Current directory lives in location.hash rather than JS state, so
    // back/forward and page refresh land on the same folder.
    _currentPath: function () {
        var hash = decodeURIComponent(location.hash.replace(/^#/, ''));
        return hash || '/';
    },

    open: function (path) {
        location.hash = encodeURIComponent(path);
    },

    _renderBreadcrumb: function (currentPath) {
        var el = document.getElementById('file-breadcrumb');
        if (!el) return;

        var segments = currentPath.split('/').filter(Boolean);
        var html = '<li class="breadcrumb-item"><a href="#" onclick=\'Distribyted.files.open("/"); return false;\'><i class="mdi mdi-home"></i></a></li>';

        var acc = '';
        segments.forEach(function (seg, i) {
            acc += '/' + seg;
            var isLast = i === segments.length - 1;
            if (isLast) {
                html += '<li class="breadcrumb-item active">' + seg + '</li>';
            } else {
                html += '<li class="breadcrumb-item"><a href="#" onclick=\'Distribyted.files.open("' + acc + '"); return false;\'>' + seg + '</a></li>';
            }
        });

        el.innerHTML = html;
    },

    confirmDelete: function (path, isDir) {
        Distribyted.confirm({
            title: isDir ? 'Delete folder' : 'Delete file',
            body: 'Delete "' + path + '"? If this is the last reference to a torrent, the torrent will be removed too.',
            confirmLabel: 'Delete',
            danger: true
        }).then((ok) => {
            if (ok) this.deleteEntry(path);
        });
    },

    deleteEntry: function (path) {
        var url = '/api/fs' + path.split('/').map(encodeURIComponent).join('/');
        Distribyted.api.del(url)
            .then(() => {
                Distribyted.message.info('Deleted.');
                this.loadView();
            })
            .catch((error) => {
                Distribyted.message.error('Error deleting: ' + error.message);
            });
    },

    promptRename: function (path) {
        var name = path.substring(path.lastIndexOf('/') + 1);
        var newName = window.prompt('Rename "' + name + '" to:', name);
        if (!newName || newName === name) return;
        var newPath = path.substring(0, path.lastIndexOf('/') + 1) + newName;
        this.renameEntry(path, newPath);
    },

    renameEntry: function (oldPath, newPath) {
        Distribyted.api.post('/api/fs/rename', { old_path: oldPath, new_path: newPath })
            .then(() => {
                Distribyted.message.info('Renamed.');
                this.loadView();
            })
            .catch((error) => {
                Distribyted.message.error('Error renaming: ' + error.message);
            });
    },

    mkdir: function () {
        var currentPath = this._currentPath();
        var name = window.prompt('New folder name:');
        if (!name) return;
        var newPath = (currentPath === '/' ? '' : currentPath) + '/' + name;
        Distribyted.api.post('/api/fs/mkdir', { path: newPath })
            .then(() => {
                Distribyted.message.info('Folder created.');
                this.loadView();
            })
            .catch((error) => {
                Distribyted.message.error('Error creating folder: ' + error.message);
            });
    },

    loadView: function () {
        var currentPath = this._currentPath();
        this._renderBreadcrumb(currentPath);

        var url = '/api/fs' + (currentPath === '/' ? '/' : currentPath);

        Distribyted.template('files')
            .then((t) => {
                return Distribyted.api.get(url).then((entries) => {
                    document.getElementById('template_target').innerHTML = t(entries || []);
                });
            })
            .catch((error) => {
                Distribyted.message.error('Error loading directory: ' + error.message);
            });
    }
};

document.getElementById('file-new-folder').addEventListener('click', function () {
    Distribyted.files.mkdir();
});

window.addEventListener('hashchange', function () {
    Distribyted.files.loadView();
});
