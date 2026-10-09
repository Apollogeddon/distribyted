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

        // the path comes from location.hash, so it is only ever set as text
        var crumb = function (label, path) {
            var li = document.createElement('li');
            li.className = 'breadcrumb-item';
            if (path === null) {
                li.classList.add('active');
                li.textContent = label;
                return li;
            }
            var a = document.createElement('a');
            a.href = '#';
            a.addEventListener('click', function (e) {
                e.preventDefault();
                Distribyted.files.open(path);
            });
            if (label === null) {
                var icon = document.createElement('i');
                icon.className = 'mdi mdi-home';
                a.appendChild(icon);
            } else {
                a.textContent = label;
            }
            li.appendChild(a);
            return li;
        };

        var segments = currentPath.split('/').filter(Boolean);
        var items = [crumb(null, '/')];
        var acc = '';
        segments.forEach(function (seg, i) {
            acc += '/' + seg;
            items.push(crumb(seg, i === segments.length - 1 ? null : acc));
        });

        el.replaceChildren.apply(el, items);
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
