# Maintainer: thisilike
pkgname=ts-status-git
pkgver=1.0.0
pkgrel=1
pkgdesc='NDJSON bridge for the TeamSpeak Remote Apps WebSocket API'
arch=('x86_64')
url='https://github.com/thisilike/ts-status'
license=('MIT')
makedepends=('go' 'git')
depends=('glibc')
provides=('ts-status')
conflicts=('ts-status')
source=("git+$url.git")
sha256sums=('SKIP')

pkgver() {
    cd ts-status
    git describe --tags --long --abbrev=7 | sed 's/^v//;s/-/.r/;s/-/./'
}

build() {
    cd ts-status
    export CGO_CPPFLAGS="$CPPFLAGS"
    export CGO_CFLAGS="$CFLAGS"
    export CGO_CXXFLAGS="$CXXFLAGS"
    export CGO_LDFLAGS="$LDFLAGS"
    export GOFLAGS="-buildmode=pie -trimpath -ldflags=-linkmode=external -mod=readonly -modcacherw"
    go build -o ts-status .
}

package() {
    cd ts-status
    install -Dm755 ts-status "$pkgdir/usr/bin/ts-status"
    install -Dm644 LICENSE "$pkgdir/usr/share/licenses/ts-status/LICENSE"
}
