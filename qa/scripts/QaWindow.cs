// QaWindow: a tiny WinForms window used by the PetAI QA harness as a
// controllable "user application" (foreground app/title for the watcher,
// click target behind the overlay, solid backdrop for screenshots, fake
// fullscreen app for DND tests). Compiled by QaCommon.ps1 with Add-Type
// (C# 5 syntax only). It never touches anything outside its own window.
//
// Args: --title T --x N --y N --w N --h N --color RRGGBB --fullscreen
//       --noactivate --topmost --clicklog PATH --lifetime SECONDS --label TEXT
using System;
using System.Drawing;
using System.IO;
using System.Runtime.InteropServices;
using System.Windows.Forms;

namespace PetQaWindow
{
    public class QaForm : Form
    {
        [DllImport("user32.dll")] static extern bool AllowSetForegroundWindow(int pid);
        [DllImport("user32.dll")] static extern bool SetProcessDpiAwarenessContext(IntPtr ctx);

        bool noActivate;
        string clickLog;
        Label label;

        public QaForm(string[] args)
        {
            string title = "QA Window";
            int x = 100, y = 100, w = 640, h = 400;
            string color = "FF00FF";
            bool fullscreen = false, topmost = false;
            int lifetime = 900;
            string labelText = null;
            for (int i = 0; i < args.Length; i++)
            {
                string a = args[i];
                string v = (i + 1 < args.Length) ? args[i + 1] : "";
                switch (a)
                {
                    case "--title": title = v; i++; break;
                    case "--x": x = int.Parse(v); i++; break;
                    case "--y": y = int.Parse(v); i++; break;
                    case "--w": w = int.Parse(v); i++; break;
                    case "--h": h = int.Parse(v); i++; break;
                    case "--color": color = v; i++; break;
                    case "--clicklog": clickLog = v; i++; break;
                    case "--lifetime": lifetime = int.Parse(v); i++; break;
                    case "--label": labelText = v; i++; break;
                    case "--fullscreen": fullscreen = true; break;
                    case "--topmost": topmost = true; break;
                    case "--noactivate": noActivate = true; break;
                }
            }
            Text = title;
            StartPosition = FormStartPosition.Manual;
            AutoScaleMode = AutoScaleMode.None;
            BackColor = ColorTranslator.FromHtml("#" + color);
            TopMost = topmost;
            ShowInTaskbar = true;
            if (fullscreen)
            {
                FormBorderStyle = FormBorderStyle.None;
                Bounds = Screen.PrimaryScreen.Bounds;
            }
            else
            {
                FormBorderStyle = FormBorderStyle.None;
                Bounds = new Rectangle(x, y, w, h);
            }
            label = new Label();
            label.AutoSize = true;
            label.Font = new Font("Segoe UI", 14f);
            label.ForeColor = Color.Black;
            label.BackColor = Color.White;
            label.Text = labelText ?? ("PetAI QA test window - safe to ignore\r\n" + title);
            label.Location = new Point(20, 20);
            Controls.Add(label);

            MouseDown += OnDown;
            label.MouseDown += OnDown;
            MouseDoubleClick += delegate(object s, MouseEventArgs e) { Log("dbl", e.Button.ToString()); };
            Activated += delegate(object s, EventArgs e) { AllowSetForegroundWindow(-1); Log("activated", ""); };
            Deactivate += delegate(object s, EventArgs e) { Log("deactivated", ""); };

            Timer t = new Timer();
            t.Interval = Math.Max(1, lifetime) * 1000;
            t.Tick += delegate(object s, EventArgs e) { Close(); };
            t.Start();
        }

        void OnDown(object sender, MouseEventArgs e)
        {
            Point p = Cursor.Position;
            Log("down", e.Button.ToString() + " " + p.X + " " + p.Y);
        }

        void Log(string kind, string detail)
        {
            if (string.IsNullOrEmpty(clickLog)) return;
            try
            {
                File.AppendAllText(clickLog, DateTime.Now.ToString("o") + " " + kind + " " + detail + "\r\n");
            }
            catch (Exception) { }
        }

        protected override bool ShowWithoutActivation
        {
            get { return noActivate; }
        }

        [STAThread]
        public static void Main(string[] args)
        {
            try { SetProcessDpiAwarenessContext(new IntPtr(-4)); } catch (Exception) { }
            Application.EnableVisualStyles();
            Application.Run(new QaForm(args));
        }
    }
}
