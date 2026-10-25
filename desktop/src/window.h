#pragma once

#include "recorder.h"
#include "text.h"

#include <QAction>
#include <QLabel>
#include <QSystemTrayIcon>
#include <QWidget>

class QGridLayout;
class QVBoxLayout;

struct Launch {
    QString api, stateDir, origin, program;
};

// The control window and the tray icon: each step from program to upload with its state and the command that fixes
// it, then the settings, the upload history and removal, as the profile page's "This computer" panel shows them.
class Window : public QWidget {
    Q_OBJECT
public:
    explicit Window(const Launch &launch);
    void present();

private:
    void render();
    void renderLive(const QJsonObject &status);
    void renderUnlinked(const QJsonObject &status);
    void renderUnreached();
    void programStep(const QJsonObject &status);
    void step(const QString &label, const QString &value, const QString &mark, QWidget *action = nullptr,
              const QString &detail = {});
    QWidget *button(const QString &label, const std::function<void()> &run, bool enabled = true,
                    const QString &kind = {});
    void settings(const QJsonObject &status);
    void history(const QJsonArray &sends);
    void removal(bool busy);
    void clear();
    void fit();
    void updateTray();
    void openRecords();
    void closeEvent(QCloseEvent *event) override;

    Launch m_launch;
    Text t;
    Recorder m_recorder;
    QVBoxLayout *m_body;
    QGridLayout *m_steps = nullptr;
    QLabel *m_error;
    bool m_asking = false;
    QSystemTrayIcon m_tray;
    QAction *m_send, *m_pause;
};
