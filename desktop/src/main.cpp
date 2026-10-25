#include "window.h"

#include <QApplication>
#include <QCommandLineParser>
#include <QDir>
#include <QFile>
#include <QFontDatabase>
#include <QLocalServer>
#include <QLocalSocket>
#include <QStyleHints>
#include <QThread>

#ifdef Q_OS_WIN
#define NOMINMAX
#include <windows.h>
#endif

#ifdef Q_OS_MACOS
void keepOutOfDock();
#endif

// One app runs per user. A second start hands its request ("show" or "tray", with its version) to the first; the
// first yields to a different version, so after an update the program's newer app replaces the running one.
static QString instanceName() {
    return "wowdb-world-recorder-" + qEnvironmentVariable("USER", qEnvironmentVariable("USERNAME"));
}

static bool handOver(const QString &request) {
#ifdef Q_OS_WIN
    // A start the player made may bring a window to the front; the running app may do so in its place.
    AllowSetForegroundWindow(ASFW_ANY);
#endif
    for (int attempt = 0; attempt < 20; ++attempt) {
        QLocalSocket socket;
        socket.connectToServer(instanceName());
        if (!socket.waitForConnected(500))
            return false;
        socket.write(request.toUtf8() + '\n');
        socket.waitForBytesWritten(500);
        socket.waitForReadyRead(1000);
        if (socket.readAll().trimmed() != "yield")
            return true;
        QThread::msleep(250);
    }
    return true;
}

static Launch launchOptions(const QCommandLineParser &args) {
    QString dir = QCoreApplication::applicationDirPath();
#ifdef Q_OS_WIN
    QString program = QDir(dir).filePath("wowdb-recorder.exe");
#else
    QString program = QDir(dir).filePath("wowdb-recorder");
#endif
    auto value = [&](const char *name, const QString &fallback) {
        return args.isSet(name) ? args.value(name) : fallback;
    };
    // The program's state folder is Go's os.UserConfigDir joined with wowdb-recorder.
#ifdef Q_OS_WIN
    QString config = qEnvironmentVariable("AppData");
#else
    QString config = QDir::home().filePath("Library/Application Support");
#endif
    return {value("api", "127.0.0.1:47930"), value("state", QDir(config).filePath("wowdb-recorder")),
            value("origin", "https://wowdb.wiki"), value("program", program)};
}

int main(int argc, char *argv[]) {
    QApplication app(argc, argv);
    QApplication::setApplicationName("World Recorder");
    QApplication::setApplicationVersion(APP_VERSION);
    QApplication::setQuitOnLastWindowClosed(false);
    // The window is dark, so its title bar is too, whatever the system's appearance.
    app.styleHints()->setColorScheme(Qt::ColorScheme::Dark);
#ifdef Q_OS_MACOS
    keepOutOfDock();
#endif
    QCommandLineParser args;
    args.addOptions({{"tray", "Start with the tray icon only."}, {"api", "Program API address.", "address"},
                     {"state", "Program state folder.", "folder"}, {"origin", "Site origin.", "url"},
                     {"program", "Recorder program.", "file"}});
    // Unknown arguments are the system's own (macOS passes -AppleLanguages and the like through).
    args.parse(QCoreApplication::arguments());

    QString mode = args.isSet("tray") ? "tray" : "show";
    if (handOver(mode + ' ' + APP_VERSION))
        return 0;
    QLocalServer::removeServer(instanceName());
    QLocalServer server;
    server.listen(instanceName());

    QFontDatabase::addApplicationFont(":/friz.ttf");
    QFontDatabase::addApplicationFont(":/morpheus.ttf");
    if (QFile style(":/style.qss"); style.open(QIODevice::ReadOnly))
        app.setStyleSheet(QString::fromUtf8(style.readAll()));
    app.setWindowIcon(QIcon(":/icon.png"));

    Window window(launchOptions(args));
    QObject::connect(&server, &QLocalServer::newConnection, &app, [&] {
        QLocalSocket *socket = server.nextPendingConnection();
        QObject::connect(socket, &QLocalSocket::disconnected, socket, &QObject::deleteLater);
        QObject::connect(socket, &QLocalSocket::readyRead, &app, [&, socket] {
            const QList<QByteArray> request = socket->readLine().trimmed().split(' ');
            if (request.value(1) != APP_VERSION) {
                socket->write("yield\n");
                socket->flush();
                server.close();
                app.quit();
                return;
            }
            socket->write("ok\n");
            if (request.value(0) == "show")
                window.present();
        });
    });
    if (mode == "show")
        window.present();
    return app.exec();
}
